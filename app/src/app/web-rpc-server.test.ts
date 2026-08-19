import { afterEach, describe, expect, test } from "bun:test";
import { RemoteAttachmentStore } from "./remote-attachment-store";
import type {
	RpcCommand,
	RpcConnectionSnapshot,
	RpcEventListener,
	RpcWriter,
} from "./rpc-session-host";
import {
	type WebBasicAuthCredentials,
	type WebRpcHost,
	WebRpcServer,
} from "./web-rpc-server";

class FakeRpcHost implements WebRpcHost {
	private readonly listeners = new Set<RpcEventListener>();
	private snapshot: RpcConnectionSnapshot = {
		state: { sessionId: "session-1", isStreaming: false },
		messages: [],
		messageOffset: 0,
		totalMessageCount: 0,
		pendingInteractions: [],
		pendingInteractionGeneration: 0,
	};

	subscribe(listener: RpcEventListener): () => void {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	getConnectionSnapshot(_maxMessages?: number): RpcConnectionSnapshot {
		return this.snapshot;
	}

	setSnapshot(snapshot: RpcConnectionSnapshot): void {
		this.snapshot = snapshot;
	}

	async handleCommand(command: RpcCommand, respond: RpcWriter): Promise<void> {
		await respond({
			...(command.id === undefined ? {} : { id: command.id }),
			type: "response",
			command: command.type,
			success: true,
			...(command.type === "get_capabilities"
				? {
						data: {
							protocolVersion: 2,
							commands: [],
							eventSequencing: { supported: false },
						},
					}
				: command.type === "get_messages"
					? {
							data: {
								messages: this.snapshot.messages,
								offset: this.snapshot.messageOffset,
								totalMessageCount: this.snapshot.totalMessageCount,
								hasMore: false,
							},
						}
					: command.type === "get_pending_interactions"
						? {
								data: {
									requests: this.snapshot.pendingInteractions,
									offset: 0,
									generation: this.snapshot.pendingInteractionGeneration,
									stale: false,
									totalRequestCount: this.snapshot.pendingInteractions.length,
									hasMore: false,
								},
							}
						: {}),
		});
	}

	emit(record: unknown): void {
		for (const listener of this.listeners) listener(record);
	}
}

type SocketConnection = {
	socket: WebSocket;
	sync: Record<string, unknown>;
	complete: Record<string, unknown>;
	replayed: unknown[];
	next(): Promise<unknown>;
};

function basicAuthorization(username: string, password: string): string {
	return `Basic ${Buffer.from(`${username}:${password}`, "utf8").toString("base64")}`;
}

async function openWebSocket(
	url: string,
	headers: Record<string, string> = {},
): Promise<SocketConnection> {
	const origin = new URL(url);
	origin.protocol = origin.protocol === "wss:" ? "https:" : "http:";
	const WebSocketWithOptions = WebSocket as unknown as new (
		url: string,
		options: Bun.WebSocketOptions,
	) => WebSocket;
	const socket = new WebSocketWithOptions(url, {
		headers: { origin: origin.origin, ...headers },
	});
	const queued: unknown[] = [];
	const waiters: Array<(record: unknown) => void> = [];
	socket.addEventListener("message", (event) => {
		const record = JSON.parse(String(event.data));
		const waiter = waiters.shift();
		if (waiter) waiter(record);
		else queued.push(record);
	});
	await new Promise<void>((resolve, reject) => {
		socket.addEventListener("open", () => resolve(), { once: true });
		socket.addEventListener(
			"error",
			() => reject(new Error("WebSocket failed to connect")),
			{ once: true },
		);
	});
	const next = (): Promise<unknown> => {
		const record = queued.shift();
		if (record !== undefined) return Promise.resolve(record);
		return new Promise((resolve, reject) => {
			const timer = setTimeout(
				() => reject(new Error("Timed out waiting for message")),
				1000,
			);
			waiters.push((value) => {
				clearTimeout(timer);
				resolve(value);
			});
		});
	};
	const sync = (await next()) as Record<string, unknown>;
	const replayed: unknown[] = [];
	let complete = (await next()) as Record<string, unknown>;
	while (complete.type !== "sync_complete") {
		replayed.push(complete);
		complete = (await next()) as Record<string, unknown>;
	}
	return { socket, sync, complete, replayed, next };
}

describe("WebRpcServer", () => {
	const servers: WebRpcServer[] = [];
	const sockets: WebSocket[] = [];

	afterEach(async () => {
		for (const server of servers.splice(0)) await server.stop();
		for (const socket of sockets.splice(0)) socket.close();
	});

	function start(
		options: {
			publicUrl?: string;
			allowedHosts?: string[];
			allowedOrigins?: string[];
			basicAuth?: WebBasicAuthCredentials;
			attachments?: RemoteAttachmentStore;
			eventStreamId?: string;
			eventHistoryMaxEvents?: number;
			eventHistoryMaxBytes?: number;
		} = {},
	) {
		const host = new FakeRpcHost();
		const server = new WebRpcServer(host, { port: 0, ...options });
		servers.push(server);
		return { host, address: server.start() };
	}

	test("serves health and the same-origin browser client", async () => {
		const { address } = start();
		const health = await fetch(`${address.url}/api/health`);
		const page = await fetch(address.url);
		const mica = await fetch(`${address.url}/assets/mica.css`);
		const font = await fetch(
			`${address.url}/assets/jetbrains-mono-normal.woff2`,
		);
		const client = await fetch(`${address.url}/assets/client.js`);

		expect(await health.json()).toEqual({ ok: true, mode: "web", clients: 0 });
		expect(page.headers.get("content-type")).toContain("text/html");
		expect(page.headers.get("content-security-policy")).toContain(
			"script-src 'self'",
		);
		expect(await page.text()).toContain('<div id="app"></div>');
		expect(mica.headers.get("content-type")).toContain("text/css");
		expect((await mica.text()).length).toBeGreaterThan(60_000);
		expect(font.headers.get("content-type")).toBe("font/woff2");
		expect((await font.arrayBuffer()).byteLength).toBeGreaterThan(10_000);
		expect(client.headers.get("content-type")).toContain("text/javascript");
		const clientJavaScript = await client.text();
		expect(clientJavaScript).toContain("new WebSocket");
		expect(clientJavaScript).toContain("Conversation transcript");
		expect(clientJavaScript).toContain("solid-js");
	});

	test("accepts a canonical public Host and Origin without forwarded headers", async () => {
		const { address } = start({ publicUrl: "https://kit.example.com" });
		const page = await fetch(address.url, {
			headers: { host: "kit.example.com" },
		});
		expect(page.status).toBe(200);
		expect(page.headers.get("content-security-policy")).toContain(
			"wss://kit.example.com",
		);
		const connection = await openWebSocket(
			`${address.url.replace("http://", "ws://")}/api/rpc`,
			{
				host: "kit.example.com",
				origin: "https://kit.example.com",
			},
		);
		sockets.push(connection.socket);
		expect(connection.sync).toMatchObject({ type: "sync", mode: "snapshot" });

		const forwardedOnly = await fetch(address.url, {
			headers: {
				host: "proxy.invalid",
				forwarded: "host=kit.example.com;proto=https",
			},
		});
		expect(forwardedOnly.status).toBe(403);
	});

	test("protects HTTP resources and WebSocket upgrades with Basic auth", async () => {
		const basicAuth = {
			username: "remote-user",
			password: "secret:with-colon",
		};
		const { address } = start({ basicAuth });
		const authorization = basicAuthorization(
			basicAuth.username,
			basicAuth.password,
		);
		const unauthorized = await fetch(address.url);
		const unauthorizedHealth = await fetch(`${address.url}/api/health`);
		const unauthorizedAsset = await fetch(`${address.url}/assets/client.css`);
		const unauthorizedAttachment = await fetch(
			`${address.url}/api/attachments/missing`,
		);
		const wrongPassword = await fetch(address.url, {
			headers: {
				authorization: basicAuthorization(basicAuth.username, "wrong"),
			},
		});
		const invalidHost = await fetch(address.url, {
			headers: { host: "example.com" },
		});

		expect(unauthorized.status).toBe(401);
		expect(unauthorized.headers.get("www-authenticate")).toBe(
			'Basic realm="Kit web mode", charset="UTF-8"',
		);
		expect(unauthorized.headers.get("cache-control")).toBe("no-store");
		expect(unauthorizedHealth.status).toBe(401);
		expect(unauthorizedAsset.status).toBe(401);
		expect(unauthorizedAttachment.status).toBe(401);
		expect(wrongPassword.status).toBe(401);
		expect(invalidHost.status).toBe(403);
		expect(invalidHost.headers.has("www-authenticate")).toBe(false);

		const page = await fetch(address.url, {
			headers: { authorization },
		});
		const health = await fetch(`${address.url}/api/health`, {
			headers: { authorization },
		});
		expect(page.status).toBe(200);
		expect(await health.json()).toEqual({
			ok: true,
			mode: "web",
			clients: 0,
		});

		const preflight = await fetch(`${address.url}/api/attachments/missing`, {
			method: "OPTIONS",
			headers: {
				origin: address.url,
				"access-control-request-headers": "authorization",
				"access-control-request-method": "DELETE",
			},
		});
		expect(preflight.status).toBe(204);
		expect(preflight.headers.get("access-control-allow-origin")).toBe(
			address.url,
		);
		expect(preflight.headers.get("access-control-allow-headers")).toContain(
			"authorization",
		);
		expect(preflight.headers.get("access-control-allow-credentials")).toBe(
			"true",
		);

		const webSocketUrl = `${address.url.replace("http://", "ws://")}/api/rpc`;
		const unauthenticatedUpgrade = await fetch(`${address.url}/api/rpc`, {
			headers: { origin: address.url },
		});
		const crossOriginUpgrade = await fetch(`${address.url}/api/rpc`, {
			headers: { origin: "https://example.com" },
		});
		expect(unauthenticatedUpgrade.status).toBe(401);
		expect(crossOriginUpgrade.status).toBe(403);
		expect(crossOriginUpgrade.headers.has("www-authenticate")).toBe(false);
		const connection = await openWebSocket(webSocketUrl, { authorization });
		sockets.push(connection.socket);
		expect(connection.sync).toMatchObject({
			type: "sync",
			mode: "snapshot",
		});
	});

	test("includes pending persistent toasts in the initial snapshot", async () => {
		const { host, address } = start({ eventStreamId: "stream-1" });
		host.emit({
			type: "ui.toast.requested",
			toast: {
				title: "Plugin failed",
				subtitle: "Initialization error",
				variant: "error",
				persistent: true,
			},
		});
		const connection = await openWebSocket(
			`${address.url.replace("http://", "ws://")}/api/rpc`,
		);
		sockets.push(connection.socket);

		expect(connection.sync.toasts).toEqual([
			{
				title: "Plugin failed",
				subtitle: "Initialization error",
				variant: "error",
				persistent: true,
			},
		]);
	});

	test("uploads and removes opaque attachments", async () => {
		const attachments = new RemoteAttachmentStore();
		const { address } = start({ attachments });
		const form = new FormData();
		form.append(
			"file",
			new File(["remote contents"], "notes.txt", { type: "text/plain" }),
		);
		const uploaded = await fetch(`${address.url}/api/attachments`, {
			method: "POST",
			headers: { origin: address.url },
			body: form,
		});
		expect(uploaded.status).toBe(201);
		expect(uploaded.headers.get("access-control-allow-origin")).toBe(
			address.url,
		);
		const payload = (await uploaded.json()) as {
			attachment: { id: string; filename: string; kind: string };
		};
		expect(payload.attachment).toMatchObject({
			filename: "notes.txt",
			kind: "text",
		});

		const downloaded = await fetch(
			`${address.url}/api/attachments/${payload.attachment.id}`,
		);
		expect(downloaded.headers.get("content-type")).toBe(
			"application/octet-stream",
		);
		expect(downloaded.headers.get("content-disposition")).toBe("attachment");
		expect(downloaded.headers.get("x-content-type-options")).toBe("nosniff");
		expect(await downloaded.text()).toBe("remote contents");

		const removed = await fetch(
			`${address.url}/api/attachments/${payload.attachment.id}`,
			{ method: "DELETE", headers: { origin: address.url } },
		);
		expect(removed.status).toBe(204);
		const missing = await fetch(
			`${address.url}/api/attachments/${payload.attachment.id}`,
			{ method: "DELETE", headers: { origin: address.url } },
		);
		expect(missing.status).toBe(404);
		attachments.dispose();
	});

	test("rejects cross-origin attachment uploads", async () => {
		const attachments = new RemoteAttachmentStore();
		const { address } = start({ attachments });
		const response = await fetch(`${address.url}/api/attachments`, {
			method: "POST",
			headers: { origin: "https://example.com" },
			body: new FormData(),
		});
		const originless = await fetch(`${address.url}/api/attachments`, {
			method: "POST",
			body: new FormData(),
		});
		expect(response.status).toBe(403);
		expect(originless.status).toBe(403);
		attachments.dispose();
	});

	test("broadcasts sequenced events to multiple clients and scopes responses", async () => {
		const { host, address } = start({ eventStreamId: "stream-1" });
		const webSocketUrl = `${address.url.replace("http://", "ws://")}/api/rpc`;
		const first = await openWebSocket(webSocketUrl);
		const second = await openWebSocket(webSocketUrl);
		sockets.push(first.socket, second.socket);
		expect(first.sync).toEqual({
			type: "sync",
			mode: "snapshot",
			reason: "initial",
			protocolVersion: 2,
			streamId: "stream-1",
			sequence: 0,
			state: { sessionId: "session-1", isStreaming: false },
			messages: [],
			messageOffset: 0,
			totalMessageCount: 0,
			pendingInteractions: [],
			pendingInteractionGeneration: 0,
			pendingInteractionOffset: 0,
			totalPendingInteractionCount: 0,
		});
		expect(first.complete).toEqual({
			type: "sync_complete",
			mode: "snapshot",
			streamId: "stream-1",
			sequence: 0,
		});

		host.emit({ type: "agent.start" });
		expect(await first.next()).toEqual({
			type: "agent.start",
			streamId: "stream-1",
			sequence: 1,
		});
		expect(await second.next()).toEqual({
			type: "agent.start",
			streamId: "stream-1",
			sequence: 1,
		});

		first.socket.send(JSON.stringify({ id: "state", type: "get_state" }));
		expect(await first.next()).toEqual({
			id: "state",
			type: "response",
			command: "get_state",
			success: true,
		});
		first.socket.send(
			JSON.stringify({ id: "capabilities", type: "get_capabilities" }),
		);
		expect(await first.next()).toEqual(
			expect.objectContaining({
				id: "capabilities",
				type: "response",
				command: "get_capabilities",
				success: true,
				data: expect.objectContaining({
					protocolVersion: 2,
					commands: ["get_message_chunk"],
					eventSequencing: {
						supported: true,
						resume: "websocket_query",
						streamId: "stream-1",
						latestSequence: 1,
						maxEvents: 2048,
						maxBytes: 8 * 1024 * 1024,
					},
					limits: expect.objectContaining({
						attachments: expect.objectContaining({
							maxConcurrentUploads: 4,
							maxRequestBytes: 11 * 1024 * 1024,
						}),
						pagination: expect.objectContaining({
							messages: { defaultPageSize: 50, maxPageSize: 50 },
						}),
						snapshot: { maxMessages: 200, maxBytes: 64 * 1024 },
						recovery: expect.objectContaining({
							message: expect.objectContaining({
								maxChunkBytes: 32 * 1024,
								maxTotalBytes: 16 * 1024 * 1024,
							}),
						}),
					}),
				}),
			}),
		);

		host.emit({ type: "agent.turn.started", turnId: "turn-1" });
		expect(await first.next()).toEqual({
			type: "agent.turn.started",
			turnId: "turn-1",
			streamId: "stream-1",
			sequence: 2,
		});
		expect(await second.next()).toEqual({
			type: "agent.turn.started",
			turnId: "turn-1",
			streamId: "stream-1",
			sequence: 2,
		});

		host.emit({
			type: "session.message.appended",
			turnId: "turn-1",
			messageId: "message-user",
			message: {
				role: "user",
				turnId: "turn-1",
				messageId: "message-user",
				content: [
					{
						type: "image",
						data: "large-base64-payload",
						mimeType: "image/png",
						filename: "image.png",
						attachmentId: "attachment-1",
						sourcePath: "/private/image.png",
					},
				],
			},
		});
		expect(await first.next()).toEqual({
			type: "session.message.appended",
			turnId: "turn-1",
			messageId: "message-user",
			streamId: "stream-1",
			sequence: 3,
			message: {
				role: "user",
				turnId: "turn-1",
				messageId: "message-user",
				content: [
					{
						type: "image",
						mimeType: "image/png",
						filename: "image.png",
						attachmentId: "attachment-1",
						dataOmitted: true,
					},
				],
			},
		});

		host.emit({ type: "agent.end", messages: [{ role: "assistant" }] });
		expect(await first.next()).toEqual({
			type: "agent.end",
			streamId: "stream-1",
			sequence: 4,
		});
		const circular: Record<string, unknown> = { type: "circular" };
		circular.self = circular;
		host.emit(circular);
		expect(await first.next()).toEqual({
			type: "circular",
			self: "[Circular]",
			streamId: "stream-1",
			sequence: 5,
		});
	});

	test("replays retained events before resuming live delivery", async () => {
		const { host, address } = start({ eventStreamId: "stream-1" });
		const webSocketUrl = `${address.url.replace("http://", "ws://")}/api/rpc`;
		const first = await openWebSocket(webSocketUrl);
		sockets.push(first.socket);
		host.emit({ type: "first" });
		host.emit({ type: "second" });
		await first.next();
		await first.next();

		const resumed = await openWebSocket(
			`${webSocketUrl}?streamId=stream-1&after=1`,
		);
		sockets.push(resumed.socket);
		expect(resumed.sync).toEqual({
			type: "sync",
			mode: "replay",
			protocolVersion: 2,
			streamId: "stream-1",
			sequence: 1,
			targetSequence: 2,
		});
		expect(resumed.replayed).toEqual([
			{
				type: "second",
				streamId: "stream-1",
				sequence: 2,
			},
		]);
		expect(resumed.complete).toEqual({
			type: "sync_complete",
			mode: "replay",
			streamId: "stream-1",
			sequence: 2,
		});

		host.emit({ type: "third" });
		expect(await resumed.next()).toEqual({
			type: "third",
			streamId: "stream-1",
			sequence: 3,
		});
	});

	test("falls back to a projected snapshot when replay history is unavailable", async () => {
		const { host, address } = start({
			eventStreamId: "stream-1",
			eventHistoryMaxEvents: 1,
		});
		host.setSnapshot({
			state: { sessionId: "session-2", isStreaming: true },
			messages: [
				{
					role: "user",
					content: [
						{
							type: "image",
							data: "base64",
							mimeType: "image/png",
							attachmentId: "attachment-1",
						},
					],
				},
			],
			messageOffset: 0,
			totalMessageCount: 1,
			pendingInteractions: [
				{
					id: "request-1",
					kind: "confirm",
					createdAt: "2026-01-01T00:00:00.000Z",
					payload: { message: "x".repeat(20 * 1024) },
				},
			],
			pendingInteractionGeneration: 1,
		});
		host.emit({ type: "first" });
		host.emit({ type: "second" });
		const webSocketUrl = `${address.url.replace("http://", "ws://")}/api/rpc`;
		const connection = await openWebSocket(
			`${webSocketUrl}?streamId=stream-1&after=0`,
		);
		sockets.push(connection.socket);

		expect(connection.sync).toEqual({
			type: "sync",
			mode: "snapshot",
			reason: "history_unavailable",
			protocolVersion: 2,
			streamId: "stream-1",
			sequence: 2,
			state: { sessionId: "session-2", isStreaming: true },
			messages: [
				{
					role: "user",
					content: [
						{
							type: "image",
							mimeType: "image/png",
							attachmentId: "attachment-1",
							dataOmitted: true,
						},
					],
				},
			],
			messageOffset: 0,
			totalMessageCount: 1,
			pendingInteractions: [
				{
					id: "request-1",
					kind: "confirm",
					createdAt: "2026-01-01T00:00:00.000Z",
					requestIndex: 0,
					payloadOmitted: true,
					recoveryCommand: "get_pending_interaction_chunk",
				},
			],
			pendingInteractionGeneration: 1,
			pendingInteractionOffset: 0,
			totalPendingInteractionCount: 1,
			pendingInteractionsTruncated: true,
		});
		connection.socket.send(
			JSON.stringify({ id: "pending", type: "get_pending_interactions" }),
		);
		expect(await connection.next()).toEqual({
			id: "pending",
			type: "response",
			command: "get_pending_interactions",
			success: true,
			data: {
				requests: [
					{
						id: "request-1",
						kind: "confirm",
						createdAt: "2026-01-01T00:00:00.000Z",
						requestIndex: 0,
						payloadOmitted: true,
						recoveryCommand: "get_pending_interaction_chunk",
					},
				],
				offset: 0,
				generation: 1,
				stale: false,
				totalRequestCount: 1,
				hasMore: false,
			},
		});
	});

	test("bounds snapshot history and reports the retained message offset", async () => {
		const { host, address } = start({ eventStreamId: "stream-1" });
		host.setSnapshot({
			state: { sessionId: "session-1" },
			messages: [
				{ id: "first", content: "a".repeat(30 * 1024) },
				{ id: "second", content: "b".repeat(30 * 1024) },
				{ id: "third", content: "c".repeat(30 * 1024) },
			],
			messageOffset: 0,
			totalMessageCount: 3,
			pendingInteractions: [],
			pendingInteractionGeneration: 0,
		});
		const webSocketUrl = `${address.url.replace("http://", "ws://")}/api/rpc`;
		const connection = await openWebSocket(webSocketUrl);
		sockets.push(connection.socket);

		expect(
			Buffer.byteLength(JSON.stringify(connection.sync), "utf8"),
		).toBeLessThan(64 * 1024);
		expect(connection.sync).toEqual(
			expect.objectContaining({
				messageOffset: 1,
				totalMessageCount: 3,
				messagesTruncated: true,
				messages: [
					expect.objectContaining({ id: "second" }),
					expect.objectContaining({ id: "third" }),
				],
			}),
		);
	});

	test("returns bounded message references for oversized transcript pages", async () => {
		const { host, address } = start({ eventStreamId: "stream-1" });
		host.setSnapshot({
			state: { sessionId: "session-1" },
			messages: [
				{
					id: "large",
					role: "assistant",
					messageId: "message-large",
					turnId: "turn-1",
					content: [
						{ type: "text", text: "x".repeat(100 * 1024) },
						{
							type: "image",
							data: "inline-base64",
							mimeType: "image/png",
							sourcePath: "/private/image.png",
						},
					],
				},
			],
			messageOffset: 0,
			totalMessageCount: 1,
			pendingInteractions: [],
			pendingInteractionGeneration: 0,
		});
		const webSocketUrl = `${address.url.replace("http://", "ws://")}/api/rpc`;
		const connection = await openWebSocket(webSocketUrl);
		sockets.push(connection.socket);
		expect(connection.sync).toEqual(
			expect.objectContaining({
				messages: [
					expect.objectContaining({
						type: "message_reference",
						role: "assistant",
						messageId: "message-large",
						turnId: "turn-1",
						messageIndex: 0,
					}),
				],
				messageOffset: 0,
			}),
		);
		connection.socket.send(
			JSON.stringify({ id: "messages", type: "get_messages", offset: 0 }),
		);
		const response = (await connection.next()) as Record<string, unknown>;
		expect(Buffer.byteLength(JSON.stringify(response), "utf8")).toBeLessThan(
			64 * 1024,
		);
		expect(response).toEqual({
			id: "messages",
			type: "response",
			command: "get_messages",
			success: true,
			data: {
				messages: [
					{
						type: "message_reference",
						role: "assistant",
						messageId: "message-large",
						turnId: "turn-1",
						messageIndex: 0,
						token: expect.any(String),
						serializedBytes: expect.any(Number),
						recoveryCommand: "get_message_chunk",
					},
				],
				offset: 0,
				totalMessageCount: 1,
				hasMore: false,
				messagesTruncated: true,
			},
		});
		const reference = (response.data as { messages: Array<{ token: string }> })
			.messages[0];
		const chunks: Buffer[] = [];
		let offset = 0;
		let complete = false;
		while (!complete) {
			connection.socket.send(
				JSON.stringify({
					id: `chunk-${offset}`,
					type: "get_message_chunk",
					token: reference.token,
					offset,
				}),
			);
			const chunk = (await connection.next()) as {
				data: {
					data: string;
					nextOffset: number;
					complete: boolean;
				};
			};
			chunks.push(Buffer.from(chunk.data.data, "base64"));
			if (chunks.length === 1) {
				host.setSnapshot({
					state: { sessionId: "session-2" },
					messages: [{ id: "replacement" }],
					messageOffset: 0,
					totalMessageCount: 1,
					pendingInteractions: [],
					pendingInteractionGeneration: 0,
				});
			}
			offset = chunk.data.nextOffset;
			complete = chunk.data.complete;
		}
		const reconstructed = JSON.parse(Buffer.concat(chunks).toString());
		expect(reconstructed).toEqual({
			id: "large",
			role: "assistant",
			messageId: "message-large",
			turnId: "turn-1",
			content: [
				{ type: "text", text: "x".repeat(100 * 1024) },
				{
					type: "image",
					mimeType: "image/png",
					dataOmitted: true,
				},
			],
		});
	});

	test("returns a fresh snapshot when the event stream changes", async () => {
		const { address } = start({ eventStreamId: "current-stream" });
		const webSocketUrl = `${address.url.replace("http://", "ws://")}/api/rpc`;
		const connection = await openWebSocket(
			`${webSocketUrl}?streamId=previous-stream&after=12`,
		);
		sockets.push(connection.socket);
		expect(connection.sync).toEqual(
			expect.objectContaining({
				type: "sync",
				mode: "snapshot",
				reason: "stream_changed",
				streamId: "current-stream",
				sequence: 0,
			}),
		);
	});

	test("rejects cross-origin and originless WebSocket upgrades", async () => {
		const { address } = start();
		const crossOrigin = await fetch(`${address.url}/api/rpc`, {
			headers: { origin: "https://example.com" },
		});
		const originless = await fetch(`${address.url}/api/rpc`);
		expect(crossOrigin.status).toBe(403);
		expect(originless.status).toBe(403);
	});

	test("keeps same-origin access when additional origins are configured", async () => {
		const { address } = start({ allowedOrigins: ["https://kit.example"] });
		const sameOrigin = await fetch(`${address.url}/api/rpc`, {
			headers: { origin: address.url },
		});
		const configuredOrigin = await fetch(`${address.url}/api/rpc`, {
			headers: { origin: "https://kit.example" },
		});
		expect(sameOrigin.status).not.toBe(403);
		expect(configuredOrigin.status).not.toBe(403);
	});

	test("supports explicit host and origin wildcards", async () => {
		const { address } = start({
			allowedHosts: ["*"],
			allowedOrigins: ["*"],
		});
		const response = await fetch(`${address.url}/api/rpc`, {
			headers: {
				host: "proxy.example",
				origin: "https://client.example",
			},
		});
		const opaqueOrigin = await fetch(`${address.url}/api/rpc`, {
			headers: { host: "proxy.example", origin: "null" },
		});
		expect(response.status).toBe(426);
		expect(opaqueOrigin.status).toBe(426);
	});

	test("keeps host and origin wildcard controls independent", async () => {
		const hostWildcard = start({ allowedHosts: ["*"] }).address;
		const blockedOrigin = await fetch(`${hostWildcard.url}/api/rpc`, {
			headers: { host: "proxy.example", origin: "https://client.example" },
		});
		const originWildcard = start({ allowedOrigins: ["*"] }).address;
		const blockedHost = await fetch(`${originWildcard.url}/api/rpc`, {
			headers: { host: "proxy.example", origin: "https://client.example" },
		});
		expect(blockedOrigin.status).toBe(403);
		expect(blockedHost.status).toBe(403);
	});
});
