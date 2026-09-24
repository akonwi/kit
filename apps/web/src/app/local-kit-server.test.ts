import { afterEach, describe, expect, test } from "bun:test";
import { mkdtemp, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import type { RpcCommand } from "@akonwi/kit-protocol";
import { SessionClient } from "@akonwi/kit-session-client";
import { SESSION_VERSION, type Session } from "../session";
import { KitServer, type KitServerOptions } from "./kit-server";
import { LocalKitServer } from "./local-kit-server";
import {
	ensureLocalServerPassword,
	getLocalServerPaths,
	LOCAL_SERVER_USERNAME,
	readLocalServerRegistration,
} from "./local-server-files";
import { getLocalServerStatus } from "./local-server-service";
import {
	LocalSessionConnection,
	resolveLocalSessionEndpoint,
} from "./local-session-connection";

const temporaryDirectories: string[] = [];

afterEach(async () => {
	await Promise.all(
		temporaryDirectories
			.splice(0)
			.map((directory) => rm(directory, { recursive: true, force: true })),
	);
});

function createSession(id: string, cwd: string): Session {
	return {
		id,
		version: SESSION_VERSION,
		cwd,
		createdAt: "2026-01-01T00:00:00.000Z",
		updatedAt: "2026-01-01T00:00:00.000Z",
		turns: [],
	};
}

function createStorage() {
	const sessions = new Map<string, Session>();
	let nextId = 1;
	const storage: NonNullable<KitServerOptions["storage"]> = {
		createSession: async (cwd) => createSession(`session-${nextId++}`, cwd),
		listAllSessions: async () =>
			[...sessions.values()].map((session) => ({
				id: session.id,
				cwd: session.cwd,
				createdAt: session.createdAt,
				updatedAt: session.updatedAt,
				messageCount: 0,
			})),
		listSessionsForCwd: async (cwd) =>
			[...sessions.values()]
				.filter((session) => session.cwd === cwd)
				.map((session) => ({
					id: session.id,
					cwd: session.cwd,
					createdAt: session.createdAt,
					updatedAt: session.updatedAt,
					messageCount: 0,
				})),
		readSession: async (id) => sessions.get(id) ?? null,
		writeSession: async (session) => {
			sessions.set(session.id, session);
		},
	};
	return storage;
}

function authHeader(password: string): string {
	return `Basic ${Buffer.from(`${LOCAL_SERVER_USERNAME}:${password}`, "utf8").toString("base64")}`;
}

async function createServer(options: KitServerOptions = {}) {
	const directory = await mkdtemp(path.join(tmpdir(), "kit-local-server-"));
	temporaryDirectories.push(directory);
	const paths = getLocalServerPaths(directory);
	const password = await ensureLocalServerPassword(paths);
	const server = new LocalKitServer(
		new KitServer("/default", { storage: createStorage(), ...options }),
		{ kitVersion: "1.2.3", password, paths },
	);
	const address = await server.start();
	return { address, password, paths, server };
}

function createRpcFixture(messages: unknown[] = []) {
	let listener: ((record: unknown) => void) | null = null;
	const startSession: NonNullable<KitServerOptions["startSession"]> = async (
		session,
	) => ({
		rpc: {
			abortAndWait: async () => {},
			connectClient: () => () => {},
			dispose: () => {},
			getConnectionSnapshot: () => ({
				state: {
					sessionId: session.id,
					isStreaming: false,
					pendingMessageCount: 0,
					pendingMessageGeneration: 0,
					pendingMessagePreviews: [],
				},
				messages,
				messageOffset: 0,
				totalMessageCount: messages.length,
				pendingInteractions: [],
				pendingInteractionGeneration: 0,
			}),
			handleCommand: async (command: RpcCommand, respond) => {
				await respond({
					id: command.id,
					type: "response",
					command: command.type,
					success: true,
					data:
						command.type === "get_capabilities"
							? {
									commands: ["get_capabilities"],
									limits: {
										attachments: {},
										pagination: { pendingInteractions: {} },
										recovery: { pendingInteraction: {} },
									},
								}
							: { echoed: command.type },
				});
			},
			subscribe: (next) => {
				listener = next;
				return () => {
					if (listener === next) listener = null;
				};
			},
		},
		dispose: async () => {},
	});
	return {
		startSession,
		emit: (record: unknown) => listener?.(record),
	};
}

async function connectWebSocket(
	url: string,
	password: string,
	instanceId: string,
): Promise<{
	records: Array<Record<string, unknown>>;
	socket: WebSocket;
}> {
	const records: Array<Record<string, unknown>> = [];
	const WebSocketWithOptions = WebSocket as unknown as new (
		url: string,
		options: Bun.WebSocketOptions,
	) => WebSocket;
	const socket = new WebSocketWithOptions(url, {
		headers: {
			authorization: authHeader(password),
			"x-kit-instance-id": instanceId,
		},
	});
	socket.addEventListener("message", (event) => {
		records.push(JSON.parse(String(event.data)) as Record<string, unknown>);
	});
	await new Promise<void>((resolve, reject) => {
		socket.addEventListener("open", () => resolve(), { once: true });
		socket.addEventListener(
			"error",
			() => reject(new Error("WebSocket failed")),
			{
				once: true,
			},
		);
	});
	return { records, socket };
}

async function waitForRecord(
	records: Array<Record<string, unknown>>,
	predicate: (record: Record<string, unknown>) => boolean,
): Promise<Record<string, unknown>> {
	const deadline = Date.now() + 2_000;
	while (Date.now() < deadline) {
		const record = records.find(predicate);
		if (record) return record;
		await Bun.sleep(5);
	}
	throw new Error("Timed out waiting for WebSocket record");
}

describe("LocalKitServer", () => {
	test("registers an authenticated loopback control plane", async () => {
		const { address, password, paths, server } = await createServer();
		const registration = await readLocalServerRegistration(paths);
		expect(registration).toMatchObject({
			url: address.url,
			instanceId: address.instanceId,
			kitVersion: "1.2.3",
			protocolVersion: 1,
		});
		expect((await stat(paths.runDir)).mode & 0o777).toBe(0o700);
		expect((await stat(paths.passwordPath)).mode & 0o777).toBe(0o600);
		expect((await stat(paths.registryPath)).mode & 0o777).toBe(0o600);

		const unauthorized = await fetch(`${address.url}/api/health`);
		expect(unauthorized.status).toBe(401);
		const authorized = await fetch(`${address.url}/api/health`, {
			headers: { authorization: authHeader(password) },
		});
		expect(await authorized.json()).toMatchObject({
			ok: true,
			instanceId: address.instanceId,
			kitVersion: "1.2.3",
			protocolVersion: 1,
		});
		expect(await getLocalServerStatus("1.2.3", paths)).toMatchObject({
			state: "running",
		});
		expect(await getLocalServerStatus("2.0.0", paths)).toMatchObject({
			state: "incompatible",
		});
		await server.stop();
		expect(await readLocalServerRegistration(paths)).toBeNull();
	});

	test("lists and creates sessions through authenticated HTTP", async () => {
		const { address, password, server } = await createServer();
		const headers = {
			authorization: authHeader(password),
			"content-type": "application/json",
		};
		const created = await fetch(`${address.url}/api/sessions`, {
			method: "POST",
			headers,
			body: JSON.stringify({ cwd: "/workspace" }),
		});
		expect(created.status).toBe(201);
		expect(await created.json()).toMatchObject({
			session: { id: "session-1", cwd: "/workspace" },
		});

		const listed = await fetch(
			`${address.url}/api/sessions?cwd=${encodeURIComponent("/workspace")}`,
			{ headers },
		);
		expect(await listed.json()).toEqual({
			sessions: [
				expect.objectContaining({ id: "session-1", cwd: "/workspace" }),
			],
		});
		await server.stop();
	});

	test("binds authenticated WebSockets to immutable sessions with replay", async () => {
		const fixture = createRpcFixture([
			{
				role: "assistant",
				messageId: "large-message",
				content: [{ type: "text", text: "x".repeat(100_000) }],
			},
		]);
		const { address, password, paths, server } = await createServer({
			startSession: fixture.startSession,
		});
		const headers = {
			authorization: authHeader(password),
			"content-type": "application/json",
		};
		const created = await fetch(`${address.url}/api/sessions`, {
			method: "POST",
			headers,
			body: JSON.stringify({ cwd: "/workspace" }),
		});
		const createdBody = (await created.json()) as {
			session: { id: string };
		};
		const rpcUrl = `${address.url.replace("http:", "ws:")}/api/sessions/${createdBody.session.id}/rpc`;
		const first = await connectWebSocket(rpcUrl, password, address.instanceId);
		const snapshot = await waitForRecord(
			first.records,
			(record) => record.type === "sync" && record.mode === "snapshot",
		);
		const messageReference = (snapshot.messages as Array<{ token: string }>)[0];
		const messageToken = messageReference?.token;
		expect(snapshot).toMatchObject({
			protocolVersion: 2,
			sessionId: createdBody.session.id,
			sequence: 0,
		});
		expect(messageReference).toMatchObject({
			type: "message_reference",
			messageId: "large-message",
			token: expect.any(String),
		});
		expect(
			Buffer.byteLength(JSON.stringify(snapshot), "utf8"),
		).toBeLessThanOrEqual(64 * 1024);
		await waitForRecord(
			first.records,
			(record) => record.type === "sync_complete",
		);

		first.socket.send(
			JSON.stringify({ id: "capabilities-1", type: "get_capabilities" }),
		);
		expect(
			await waitForRecord(
				first.records,
				(record) => record.id === "capabilities-1",
			),
		).toMatchObject({
			success: true,
			data: {
				commands: expect.arrayContaining(["get_message_chunk"]),
				eventSequencing: {
					supported: true,
					streamId: snapshot.streamId,
				},
			},
		});

		first.socket.send(
			JSON.stringify({
				id: "chunk-1",
				type: "get_message_chunk",
				token: messageToken,
				offset: 0,
			}),
		);
		expect(
			await waitForRecord(first.records, (record) => record.id === "chunk-1"),
		).toMatchObject({
			type: "response",
			command: "get_message_chunk",
			success: true,
			data: { offset: 0, complete: false },
		});

		first.socket.send(JSON.stringify({ id: "command-1", type: "get_state" }));
		expect(
			await waitForRecord(first.records, (record) => record.id === "command-1"),
		).toMatchObject({
			type: "response",
			command: "get_state",
			success: true,
		});
		fixture.emit({ type: "state_changed", state: { isStreaming: false } });
		const event = await waitForRecord(
			first.records,
			(record) => record.type === "state_changed",
		);
		expect(event).toMatchObject({
			streamId: snapshot.streamId,
			sequence: 1,
		});
		const unprojectable = {};
		Object.defineProperty(unprojectable, "type", {
			enumerable: true,
			get: () => {
				throw new Error("projection failed");
			},
		});
		fixture.emit(unprojectable);
		expect(
			await waitForRecord(
				first.records,
				(record) => record.type === "resync_required",
			),
		).toMatchObject({
			reason: "event_projection_failed",
			sequence: 2,
		});
		first.socket.close();

		const resumed = await connectWebSocket(
			`${rpcUrl}?streamId=${encodeURIComponent(String(snapshot.streamId))}&after=0`,
			password,
			address.instanceId,
		);
		expect(
			await waitForRecord(
				resumed.records,
				(record) => record.type === "sync" && record.mode === "replay",
			),
		).toMatchObject({ sequence: 0, targetSequence: 2 });
		expect(
			await waitForRecord(
				resumed.records,
				(record) => record.type === "state_changed",
			),
		).toMatchObject({ sequence: 1 });
		resumed.socket.close();

		const registration = await readLocalServerRegistration(paths);
		expect(registration).not.toBeNull();
		if (!registration) throw new Error("Missing local server registration");
		const client = new SessionClient(
			createdBody.session.id,
			new LocalSessionConnection({
				resolveEndpoint: () => resolveLocalSessionEndpoint(paths),
				sessionId: createdBody.session.id,
			}),
		);
		await client.connect();
		expect(client.state).toMatchObject({
			phase: "live",
			streamId: snapshot.streamId,
			sequence: 2,
		});
		expect(await client.command({ type: "get_state" })).toMatchObject({
			data: { echoed: "get_state" },
		});
		client.close();
		await server.stop();
	});

	test("stops only when shutdown targets the registered instance", async () => {
		const { address, password, paths, server } = await createServer();
		const rejected = await fetch(`${address.url}/api/shutdown`, {
			method: "POST",
			headers: {
				authorization: authHeader(password),
				"x-kit-instance-id": "stale-instance",
			},
		});
		expect(rejected.status).toBe(409);
		expect(await getLocalServerStatus("1.2.3", paths)).toMatchObject({
			state: "running",
		});
		const response = await fetch(`${address.url}/api/shutdown`, {
			method: "POST",
			headers: {
				authorization: authHeader(password),
				"x-kit-instance-id": address.instanceId,
			},
		});
		expect(response.status).toBe(200);
		await server.waitForStop();
		expect(await readLocalServerRegistration(paths)).toBeNull();
	});
});
