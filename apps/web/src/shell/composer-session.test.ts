import { describe, expect, test } from "bun:test";
import type { RpcCommand } from "@akonwi/kit-protocol";
import {
	SessionClient,
	type SessionConnection,
	type SessionConnectionStartOptions,
} from "@akonwi/kit-session-client";
import { createSessionClientComposerSession } from "./composer-session";

class RecordingConnection implements SessionConnection {
	readonly commands: RpcCommand[] = [];
	private options: SessionConnectionStartOptions | null = null;

	constructor(
		private readonly messages: unknown[] = [
			{ role: "user", content: "hello", timestamp: 1 },
		],
		private dropRestoreResponse = false,
		private dropAcknowledgementResponse = false,
		private readonly capabilityCommands: string[] = [
			"prompt",
			"follow_up",
			"execute_bash",
			"promote_follow_ups",
			"abort",
		],
		private rejectNextBash = false,
	) {}

	async start(options: SessionConnectionStartOptions): Promise<void> {
		this.options = options;
		options.onRecord({
			type: "sync",
			mode: "snapshot",
			protocolVersion: 2,
			sessionId: "session-1",
			streamId: "stream-1",
			sequence: 0,
			state: {
				sessionId: "session-1",
				cwd: "/workspace",
				isStreaming: false,
				pendingMessageCount: 2,
				pendingMessageGeneration: 4,
				pendingMessagePreviews: ["first", "second"],
			},
			messages: this.messages,
			messageOffset: 0,
			totalMessageCount: this.messages.length,
			pendingInteractions: [],
			pendingInteractionGeneration: 0,
		});
		options.onRecord({
			type: "sync_complete",
			sessionId: "session-1",
			streamId: "stream-1",
			sequence: 0,
		});
	}

	send(command: RpcCommand): void {
		this.commands.push(command);
		if (command.type === "restore_follow_ups" && this.dropRestoreResponse) {
			this.dropRestoreResponse = false;
			return;
		}
		if (
			command.type === "acknowledge_follow_up_mutation" &&
			this.dropAcknowledgementResponse
		) {
			this.dropAcknowledgementResponse = false;
			return;
		}
		const rejectBash = command.type === "execute_bash" && this.rejectNextBash;
		if (rejectBash) this.rejectNextBash = false;
		const data =
			command.type === "get_capabilities"
				? {
						commands: this.capabilityCommands,
						limits: {
							attachments: {},
							pagination: { messages: {}, pendingInteractions: {} },
							recovery: { message: {}, pendingInteraction: {} },
						},
					}
				: command.type === "promote_follow_ups"
					? { sessionId: "session-1", generation: 5, count: 2 }
					: command.type === "restore_follow_ups"
						? {
								clientId: command.clientId,
								operationId: command.operationId,
								sessionId: "session-1",
								generation: 5,
								messages: ["first", "second"],
							}
						: command.type === "acknowledge_follow_up_mutation"
							? {
									clientId: command.clientId,
									operationId: command.operationId,
									acknowledged: true,
								}
							: undefined;
		queueMicrotask(() => {
			this.options?.onRecord({
				id: command.id,
				type: "response",
				command: command.type,
				success: !rejectBash,
				...(rejectBash ? { error: "bash rejected" } : {}),
				...(data === undefined ? {} : { data }),
			});
		});
	}

	emit(record: unknown): void {
		this.options?.onRecord(record);
	}

	close(): void {}
}

describe("session-client composer session", () => {
	test("routes text, bash, queue, and lifecycle actions through the client", async () => {
		const connection = new RecordingConnection();
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		await client.connect();
		let quit = false;
		const session = createSessionClientComposerSession(client, () => {
			quit = true;
		});

		expect(session.state()).toMatchObject({
			id: "session-1",
			cwd: "/workspace",
			pendingMessageCount: 2,
			pendingMessageGeneration: 4,
			messages: [{ role: "user", content: "hello", timestamp: 1 }],
		});
		await session.submitMessage([{ type: "text", text: "next" }]);
		await session.executeBash("pwd", true);
		await session.sendFollowUp("later");
		await session.promotePendingFollowUpsToSteering();
		const restoration = await session.restorePendingMessages();
		expect(restoration.messages).toEqual([
			{ role: "user", content: "first" },
			{ role: "user", content: "second" },
		]);
		await restoration.acknowledge();
		await session.abort();
		session.quit();

		expect(
			connection.commands
				.filter((command) => command.type !== "get_capabilities")
				.map(({ id: _, ...command }) => command),
		).toEqual([
			{ type: "prompt", message: "next" },
			expect.objectContaining({
				type: "execute_bash",
				command: "pwd",
				excludeFromContext: true,
			}),
			expect.objectContaining({
				type: "acknowledge_bash_execution",
			}),
			{ type: "follow_up", message: "later" },
			{
				type: "promote_follow_ups",
				sessionId: "session-1",
				expectedGeneration: 4,
			},
			expect.objectContaining({
				type: "restore_follow_ups",
				sessionId: "session-1",
				expectedGeneration: 4,
			}),
			expect.objectContaining({
				type: "acknowledge_follow_up_mutation",
			}),
			{ type: "abort" },
		]);
		expect(quit).toBe(true);
		client.close();
	});

	test("reuses restoration identity after lost restore and acknowledgement responses", async () => {
		const connection = new RecordingConnection([], true, true);
		const client = new SessionClient("session-1", connection, {
			commandTimeoutMs: 5,
			reconnect: false,
		});
		await client.connect();
		const session = createSessionClientComposerSession(client, () => {});
		await expect(session.restorePendingMessages()).rejects.toThrow("timed out");
		const restoration = await session.restorePendingMessages();
		await restoration.acknowledge();
		const restoreCommands = connection.commands.filter(
			(command) => command.type === "restore_follow_ups",
		);
		const acknowledgementCommands = connection.commands.filter(
			(command) => command.type === "acknowledge_follow_up_mutation",
		);
		expect(restoreCommands).toHaveLength(2);
		expect(restoreCommands[0]?.operationId).toBe(
			restoreCommands[1]?.operationId,
		);
		expect(restoreCommands[0]?.expectedGeneration).toBe(
			restoreCommands[1]?.expectedGeneration,
		);
		expect(acknowledgementCommands).toHaveLength(2);
		expect(acknowledgementCommands[0]?.operationId).toBe(
			acknowledgementCommands[1]?.operationId,
		);
		client.close();
	});

	test("retains binding and normalizes messages while the client rebases", async () => {
		const connection = new RecordingConnection([
			{
				role: "user",
				content: [
					{ type: "text", text: 42 },
					{ type: "image", data: 7, mimeType: "image/png" },
				],
			},
		]);
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		await client.connect();
		const session = createSessionClientComposerSession(client, () => {});
		expect(session.state().messages).toEqual([
			{
				role: "user",
				content: [{ type: "text" }, { type: "image", mimeType: "image/png" }],
			},
		]);
		connection.emit({
			type: "resync_required",
			reason: "test",
			streamId: "stream-1",
			sequence: 1,
		});
		expect(session.state()).toMatchObject({
			id: "session-1",
			cwd: "/workspace",
		});
		client.close();
	});

	test("clears operation identity after authoritative bash rejection", async () => {
		const connection = new RecordingConnection(
			[],
			false,
			false,
			["execute_bash"],
			true,
		);
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		await client.connect();
		const session = createSessionClientComposerSession(client, () => {});
		await expect(session.executeBash("first", false)).rejects.toThrow(
			"bash rejected",
		);
		await expect(session.executeBash("second", false)).resolves.toBeUndefined();
		const operations = connection.commands.filter(
			(command) => command.type === "execute_bash",
		);
		expect(operations[0]?.operationId).not.toBe(operations[1]?.operationId);
		client.close();
	});

	test("disables composer bash behavior when the host omits the capability", async () => {
		const connection = new RecordingConnection([], false, false, ["prompt"]);
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		await client.connect();
		const session = createSessionClientComposerSession(client, () => {});
		expect(session.state().bashExecution).toBe(false);
		client.close();
	});

	test("rejects attachments until the client attachment facet is available", async () => {
		const connection = new RecordingConnection();
		const client = new SessionClient("session-1", connection, {
			reconnect: false,
		});
		await client.connect();
		const session = createSessionClientComposerSession(client, () => {});
		await expect(
			session.submitMessage([
				{
					type: "image",
					data: "base64",
					mimeType: "image/png",
				},
			]),
		).rejects.toThrow("does not yet support image attachments");
		client.close();
	});
});
