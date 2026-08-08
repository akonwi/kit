import { afterEach, describe, expect, test } from "bun:test";
import { RemoteAttachmentStore } from "./remote-attachment-store";
import type {
	RpcCommand,
	RpcEventListener,
	RpcWriter,
} from "./rpc-session-host";
import { type WebRpcHost, WebRpcServer } from "./web-rpc-server";

class FakeRpcHost implements WebRpcHost {
	private readonly listeners = new Set<RpcEventListener>();

	subscribe(listener: RpcEventListener): () => void {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	async handleCommand(command: RpcCommand, respond: RpcWriter): Promise<void> {
		await respond({
			...(command.id === undefined ? {} : { id: command.id }),
			type: "response",
			command: command.type,
			success: true,
		});
	}

	emit(record: unknown): void {
		for (const listener of this.listeners) listener(record);
	}
}

function openWebSocket(url: string): Promise<WebSocket> {
	return new Promise((resolve, reject) => {
		const origin = new URL(url);
		origin.protocol = origin.protocol === "wss:" ? "https:" : "http:";
		const WebSocketWithOptions = WebSocket as unknown as new (
			url: string,
			options: Bun.WebSocketOptions,
		) => WebSocket;
		const socket = new WebSocketWithOptions(url, {
			headers: { origin: origin.origin },
		});
		socket.addEventListener("open", () => resolve(socket), { once: true });
		socket.addEventListener(
			"error",
			() => reject(new Error("WebSocket failed to connect")),
			{ once: true },
		);
	});
}

function nextMessage(socket: WebSocket): Promise<unknown> {
	return new Promise((resolve, reject) => {
		const timer = setTimeout(
			() => reject(new Error("Timed out waiting for message")),
			1000,
		);
		socket.addEventListener(
			"message",
			(event) => {
				clearTimeout(timer);
				resolve(JSON.parse(String(event.data)));
			},
			{ once: true },
		);
	});
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
			allowedOrigins?: string[];
			attachments?: RemoteAttachmentStore;
		} = {},
	) {
		const host = new FakeRpcHost();
		const server = new WebRpcServer(host, { port: 0, ...options });
		servers.push(server);
		return { host, address: server.start() };
	}

	test("serves health and the browser entry point", async () => {
		const { address } = start();
		const health = await fetch(`${address.url}/api/health`);
		const page = await fetch(address.url);

		expect(await health.json()).toEqual({ ok: true, mode: "web", clients: 0 });
		expect(page.headers.get("content-type")).toContain("text/html");
		expect(await page.text()).toContain("Kit web mode");
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

	test("broadcasts events to multiple clients and scopes responses", async () => {
		const { host, address } = start();
		const webSocketUrl = `${address.url.replace("http://", "ws://")}/api/rpc`;
		const first = await openWebSocket(webSocketUrl);
		const second = await openWebSocket(webSocketUrl);
		sockets.push(first, second);

		const firstEvent = nextMessage(first);
		const secondEvent = nextMessage(second);
		host.emit({ type: "agent_start" });
		expect(await firstEvent).toEqual({ type: "agent_start" });
		expect(await secondEvent).toEqual({ type: "agent_start" });

		const firstResponse = nextMessage(first);
		first.send(JSON.stringify({ id: "state", type: "get_state" }));
		expect(await firstResponse).toEqual({
			id: "state",
			type: "response",
			command: "get_state",
			success: true,
		});

		const firstNext = nextMessage(first);
		const secondNext = nextMessage(second);
		host.emit({ type: "turn_start" });
		expect(await firstNext).toEqual({ type: "turn_start" });
		expect(await secondNext).toEqual({ type: "turn_start" });

		const projectedImage = nextMessage(first);
		host.emit({
			type: "message_end",
			message: {
				role: "user",
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
		expect(await projectedImage).toEqual({
			type: "message_end",
			message: {
				role: "user",
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

		const projectedEvent = nextMessage(first);
		host.emit({ type: "agent_end", messages: [{ role: "assistant" }] });
		expect(await projectedEvent).toEqual({ type: "agent_end" });
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

	test("uses configured origins instead of implicitly allowing backend HTTP", async () => {
		const { address } = start({ allowedOrigins: ["https://kit.example"] });
		const response = await fetch(`${address.url}/api/rpc`, {
			headers: { origin: address.url },
		});
		expect(response.status).toBe(403);
	});
});
