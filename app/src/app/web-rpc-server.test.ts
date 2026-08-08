import { afterEach, describe, expect, test } from "bun:test";
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

	function start(options: { allowedOrigins?: string[] } = {}) {
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

		const secondNext = nextMessage(second);
		host.emit({ type: "turn_start" });
		expect(await secondNext).toEqual({ type: "turn_start" });

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
