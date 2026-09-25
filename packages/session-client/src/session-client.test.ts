import { describe, expect, test } from "bun:test";
import type { RpcCommand } from "@akonwi/kit-protocol";
import {
	SessionClient,
	type SessionConnection,
	type SessionConnectionStartOptions,
} from "./session-client";

function snapshot(sessionId = "session-1", sequence = 0) {
	return {
		type: "sync",
		mode: "snapshot",
		protocolVersion: 2,
		sessionId,
		streamId: "stream-1",
		sequence,
		state: {
			sessionId,
			isStreaming: false,
			pendingMessageCount: 0,
			pendingMessageGeneration: 0,
			pendingMessagePreviews: [],
		},
		messages: [],
		messageOffset: 0,
		totalMessageCount: 0,
		pendingInteractions: [],
		pendingInteractionGeneration: 0,
	};
}

const validCapabilities = {
	commands: ["get_capabilities"],
	limits: {
		attachments: {},
		pagination: { messages: {}, pendingInteractions: {} },
		recovery: { message: {}, pendingInteraction: {} },
	},
};

class TestConnection implements SessionConnection {
	starts: SessionConnectionStartOptions[] = [];
	sent: RpcCommand[] = [];
	closed = false;

	constructor(
		private readonly synchronize: (
			options: SessionConnectionStartOptions,
			start: number,
		) => void = (options) => {
			options.onRecord(snapshot());
			options.onRecord({
				type: "sync_complete",
				sessionId: "session-1",
				streamId: "stream-1",
				sequence: 0,
			});
		},
		private readonly capabilities: unknown = validCapabilities,
	) {}

	async start(options: SessionConnectionStartOptions): Promise<void> {
		this.closed = false;
		this.starts.push(options);
		this.synchronize(options, this.starts.length);
	}

	send(command: RpcCommand): void {
		this.sent.push(command);
		if (command.type === "get_capabilities") {
			queueMicrotask(() => {
				this.starts.at(-1)?.onRecord({
					id: command.id,
					type: "response",
					command: command.type,
					success: true,
					data: this.capabilities,
				});
			});
		}
	}

	close(): void {
		this.closed = true;
	}
}

describe("SessionClient", () => {
	test("synchronizes state and correlates successful commands", async () => {
		const connection = new TestConnection();
		const client = new SessionClient("session-1", connection);
		const phases: string[] = [];
		client.subscribe((state) => phases.push(state.phase));
		await client.connect();
		expect(client.state.phase).toBe("live");
		expect(phases).toEqual([
			"disconnected",
			"connecting",
			"synchronizing",
			"live",
		]);

		const response = client.command({ type: "get_state" });
		const sent = connection.sent.at(-1);
		expect(sent).toMatchObject({ type: "get_state" });
		expect(typeof sent?.id).toBe("string");
		connection.starts[0]?.onRecord({
			id: sent?.id,
			type: "response",
			command: "get_state",
			success: true,
			data: { ready: true },
		});
		expect(await response).toMatchObject({ data: { ready: true } });
		client.close();
	});

	test("observes synchronization rejection when startup closes before opening", async () => {
		const unhandled: unknown[] = [];
		const onUnhandled = (error: unknown) => unhandled.push(error);
		process.on("unhandledRejection", onUnhandled);
		const connection: SessionConnection = {
			start: async (options) => {
				options.onClose(new Error("closed before open"));
				throw new Error("upgrade rejected");
			},
			send: () => {},
			close: () => {},
		};
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		try {
			await expect(client.connect()).rejects.toBeInstanceOf(Error);
			await Bun.sleep(0);
			expect(unhandled).toEqual([]);
		} finally {
			process.off("unhandledRejection", onUnhandled);
			client.close();
		}
	});

	test("stays synchronizing and closes when capability negotiation fails", async () => {
		const connection = new TestConnection(undefined, {});
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		const phases: string[] = [];
		client.subscribe((state) => phases.push(state.phase));
		await expect(client.connect()).rejects.toThrow(
			"Capabilities omitted limits",
		);
		expect(connection.closed).toBe(true);
		expect(phases).not.toContain("live");
	});

	test("rejects failed commands and every pending command on disconnect", async () => {
		const connection = new TestConnection();
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		await client.connect();
		const failed = client.command({ type: "set_model" });
		connection.starts[0]?.onRecord({
			id: connection.sent.at(-1)?.id,
			type: "response",
			command: "set_model",
			success: false,
			error: "model rejected",
		});
		await expect(failed).rejects.toThrow("model rejected");

		const pending = client.command({ type: "get_state" });
		connection.starts[0]?.onClose(new Error("socket lost"));
		await expect(pending).rejects.toThrow("socket lost");
		expect(client.state).toMatchObject({
			phase: "disconnected",
			lastError: "socket lost",
		});
	});

	test("reconnects from the last reduced stream cursor", async () => {
		const connection = new TestConnection((options, start) => {
			if (start === 1) {
				options.onRecord(snapshot());
				options.onRecord({
					type: "sync_complete",
					sessionId: "session-1",
					streamId: "stream-1",
					sequence: 0,
				});
				return;
			}
			expect(options.cursor).toEqual({ streamId: "stream-1", sequence: 1 });
			options.onRecord({
				type: "sync",
				mode: "replay",
				protocolVersion: 2,
				sessionId: "session-1",
				streamId: "stream-1",
				sequence: 1,
				targetSequence: 1,
			});
			options.onRecord({
				type: "sync_complete",
				sessionId: "session-1",
				streamId: "stream-1",
				sequence: 1,
			});
		});
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		await client.connect();
		connection.starts[0]?.onRecord({
			type: "state_changed",
			streamId: "stream-1",
			sequence: 1,
			state: {
				sessionId: "session-1",
				isStreaming: false,
				pendingMessageCount: 0,
				pendingMessageGeneration: 0,
				pendingMessagePreviews: [],
			},
		});
		connection.starts[0]?.onClose(new Error("socket lost"));
		await client.connect();
		expect(client.state).toMatchObject({ phase: "live", sequence: 1 });
	});

	test("automatically reconnects with bounded replay state after disconnect", async () => {
		const connection = new TestConnection((options) => {
			const sequence = options.cursor?.sequence ?? 0;
			options.onRecord(
				options.cursor
					? {
							type: "sync",
							mode: "replay",
							protocolVersion: 2,
							sessionId: "session-1",
							streamId: "stream-1",
							sequence,
							targetSequence: sequence,
						}
					: snapshot(),
			);
			options.onRecord({
				type: "sync_complete",
				sessionId: "session-1",
				streamId: "stream-1",
				sequence,
			});
		});
		const client = new SessionClient("session-1", connection);
		await client.connect();
		connection.starts[0]?.onClose(new Error("socket lost"));
		const deadline = Date.now() + 1_000;
		while (connection.starts.length < 2 && Date.now() < deadline) {
			await Bun.sleep(10);
		}
		expect(connection.starts[1]?.cursor).toEqual({
			streamId: "stream-1",
			sequence: 0,
		});
		expect(client.state.phase).toBe("live");
		client.close();
	});

	test("rebases from a snapshot after transcript or serialization invalidation", async () => {
		for (const invalidation of [
			{ type: "session.transcript.replaced" },
			{ type: "resync_required", reason: "event_serialization_failed" },
		]) {
			const connection = new TestConnection((options, start) => {
				options.onRecord(snapshot("session-1", start - 1));
				options.onRecord({
					type: "sync_complete",
					sessionId: "session-1",
					streamId: "stream-1",
					sequence: start - 1,
				});
			});
			const client = new SessionClient("session-1", connection);
			await client.connect();
			connection.starts[0]?.onRecord({
				...invalidation,
				streamId: "stream-1",
				sequence: 1,
			});
			const deadline = Date.now() + 1_000;
			while (connection.starts.length < 2 && Date.now() < deadline) {
				await Bun.sleep(5);
			}
			expect(connection.starts[1]?.cursor).toBeUndefined();
			expect(client.state.phase).toBe("live");
			client.close();
		}
	});

	test("closes connections with incompatible protocol or session binding", async () => {
		for (const invalid of [
			{ ...snapshot(), protocolVersion: 999 },
			{ ...snapshot(), sessionId: "session-2" },
		]) {
			const connection = new TestConnection((options) => {
				options.onRecord(invalid);
			});
			const client = new SessionClient("session-1", connection);
			await expect(client.connect()).rejects.toBeInstanceOf(Error);
			expect(connection.closed).toBe(true);
			client.close();
		}
	});
});
