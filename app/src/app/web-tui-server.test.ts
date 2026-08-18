import { afterEach, describe, expect, test } from "bun:test";
import {
	type WebTuiClient,
	type WebTuiHost,
	WebTuiServer,
} from "./web-tui-server";

type HostLog = {
	attached: { client: WebTuiClient; cols: number; rows: number }[];
	detached: WebTuiClient[];
	inputs: Uint8Array[];
	resizes: { cols: number; rows: number }[];
};

function recordingHost(): WebTuiHost & { log: HostLog } {
	const log: HostLog = { attached: [], detached: [], inputs: [], resizes: [] };
	return {
		log,
		attach: (client, cols, rows) => log.attached.push({ client, cols, rows }),
		detach: (client) => log.detached.push(client),
		input: (bytes) => {
			log.inputs.push(bytes);
			return true;
		},
		resize: (cols, rows) => log.resizes.push({ cols, rows }),
	};
}

const servers: WebTuiServer[] = [];

function startServer(
	host: WebTuiHost,
	options?: ConstructorParameters<typeof WebTuiServer>[1],
): { server: WebTuiServer; origin: string; wsUrl: string } {
	const server = new WebTuiServer(host, { port: 0, ...options });
	servers.push(server);
	const started = server.start();
	const origin = `http://127.0.0.1:${started.port}`;
	return { server, origin, wsUrl: `ws://127.0.0.1:${started.port}/api/tui` };
}

afterEach(async () => {
	for (const server of servers.splice(0)) await server.stop();
});

const WebSocketWithOptions = WebSocket as unknown as new (
	url: string,
	options?: Bun.WebSocketOptions,
) => WebSocket;

function openSocket(
	url: string,
	options?: { headers?: Record<string, string> },
): Promise<WebSocket> {
	return new Promise((resolve, reject) => {
		const socket = new WebSocketWithOptions(url, options);
		socket.binaryType = "arraybuffer";
		socket.addEventListener("open", () => resolve(socket));
		socket.addEventListener("error", (event) =>
			reject(new Error(`WebSocket failed: ${String(event)}`)),
		);
	});
}

function nextClose(socket: WebSocket): Promise<CloseEvent> {
	return new Promise((resolve) =>
		socket.addEventListener("close", resolve, { once: true }),
	);
}

async function waitFor(
	condition: () => boolean,
	timeoutMs = 2_000,
): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	while (!condition()) {
		if (Date.now() > deadline) throw new Error("timed out waiting");
		await new Promise((resolve) => setTimeout(resolve, 10));
	}
}

describe("WebTuiServer HTTP", () => {
	test("serves the TUI document with a wasm-capable same-origin CSP", async () => {
		const { origin } = startServer(recordingHost());
		const response = await fetch(`${origin}/`);
		expect(response.status).toBe(200);
		const csp = response.headers.get("content-security-policy") ?? "";
		expect(csp).toContain("default-src 'self'");
		expect(csp).toContain("script-src 'self' 'wasm-unsafe-eval'");
		expect(csp).toContain("frame-ancestors 'none'");
		const html = await response.text();
		expect(html).toContain('id="terminal"');
		expect(html).toContain("/assets/tui-client.js");
	});

	test("serves first-party terminal assets", async () => {
		const { origin } = startServer(recordingHost());
		const css = await fetch(`${origin}/assets/tui.css`);
		expect(css.status).toBe(200);
		expect(await css.text()).toContain("#terminal");
		const wasm = await fetch(`${origin}/assets/ghostty-vt.wasm`);
		expect(wasm.status).toBe(200);
		expect(wasm.headers.get("content-type")).toBe("application/wasm");
		const bytes = new Uint8Array(await wasm.arrayBuffer());
		// WASM magic number: \0asm
		expect([...bytes.slice(0, 4)]).toEqual([0x00, 0x61, 0x73, 0x6d]);
	});

	test("reports health with experimental marker", async () => {
		const { origin } = startServer(recordingHost());
		const health = await fetch(`${origin}/api/health`);
		expect(await health.json()).toEqual({
			ok: true,
			mode: "web-tui",
			experimental: true,
			clients: 0,
		});
	});

	test("rejects disallowed Host headers", async () => {
		const { origin } = startServer(recordingHost());
		const response = await fetch(`${origin}/`, {
			headers: { host: "evil.example:80" },
		});
		expect(response.status).toBe(403);
	});

	test("requires basic authentication when configured", async () => {
		const { origin } = startServer(recordingHost(), {
			basicAuth: { username: "kit", password: "secret" },
		});
		const denied = await fetch(`${origin}/`);
		expect(denied.status).toBe(401);
		expect(denied.headers.get("www-authenticate")).toContain("Basic");
		const allowed = await fetch(`${origin}/`, {
			headers: {
				authorization: `Basic ${Buffer.from("kit:secret").toString("base64")}`,
			},
		});
		expect(allowed.status).toBe(200);
	});

	test("returns 404 for unknown paths", async () => {
		const { origin } = startServer(recordingHost());
		expect((await fetch(`${origin}/unknown`)).status).toBe(404);
	});
});

describe("WebTuiServer WebSocket", () => {
	test("rejects upgrades from disallowed origins", async () => {
		const { origin } = startServer(recordingHost());
		const response = await fetch(`${origin}/api/tui`, {
			headers: {
				origin: "https://evil.example",
				upgrade: "websocket",
				connection: "Upgrade",
			},
		});
		expect(response.status).toBe(403);
	});

	test("attaches on init, forwards input and resize, detaches on close", async () => {
		const host = recordingHost();
		const { wsUrl, origin } = startServer(host);
		const socket = await openSocket(wsUrl, { headers: { origin } });

		socket.send(JSON.stringify({ type: "init", cols: 120, rows: 40 }));
		await waitFor(() => host.log.attached.length === 1);
		expect(host.log.attached[0]).toMatchObject({ cols: 120, rows: 40 });

		socket.send(new TextEncoder().encode("k"));
		await waitFor(() => host.log.inputs.length === 1);
		expect(Buffer.from(host.log.inputs[0] ?? []).toString("utf8")).toBe("k");

		socket.send(JSON.stringify({ type: "resize", cols: 90, rows: 30 }));
		await waitFor(() => host.log.resizes.length === 1);
		expect(host.log.resizes[0]).toEqual({ cols: 90, rows: 30 });

		const closed = nextClose(socket);
		socket.close();
		await closed;
		await waitFor(() => host.log.detached.length === 1);
		expect(host.log.detached[0]).toBe(host.log.attached[0]?.client);
	});

	test("sends the current browser theme on init and on later changes", async () => {
		const host = recordingHost();
		const { server, wsUrl, origin } = startServer(host);
		const firstTheme = {
			background: "#0a0a0a",
			foreground: "#fafafa",
			cursor: "#fafafa",
			selectionBackground: "#404040",
			statusBackground: "#171717",
			statusForeground: "#d4d4d4",
			statusBorder: "#404040",
		};
		server.setBrowserTheme(firstTheme);
		const socket = await openSocket(wsUrl, { headers: { origin } });
		const messages: string[] = [];
		socket.addEventListener("message", (event) => {
			if (typeof event.data === "string") messages.push(event.data);
		});
		socket.send(JSON.stringify({ type: "init", cols: 80, rows: 24 }));
		await waitFor(() => messages.length === 1);
		expect(JSON.parse(messages[0] ?? "null")).toEqual({
			type: "theme",
			theme: firstTheme,
		});

		const nextTheme = { ...firstTheme, background: "#fdf6e3" };
		server.setBrowserTheme(nextTheme);
		await waitFor(() => messages.length === 2);
		expect(JSON.parse(messages[1] ?? "null")).toEqual({
			type: "theme",
			theme: nextTheme,
		});
		socket.close();
	});

	test("delivers host output to the attached client as binary frames", async () => {
		const host = recordingHost();
		const { wsUrl, origin } = startServer(host);
		const socket = await openSocket(wsUrl, { headers: { origin } });
		const frames: Uint8Array[] = [];
		socket.addEventListener("message", (event) => {
			if (event.data instanceof ArrayBuffer) {
				frames.push(new Uint8Array(event.data));
			}
		});
		socket.send(JSON.stringify({ type: "init", cols: 80, rows: 24 }));
		await waitFor(() => host.log.attached.length === 1);
		host.log.attached[0]?.client.send(new TextEncoder().encode("\x1b[2Jhi"));
		await waitFor(() => frames.length === 1);
		expect(Buffer.from(frames[0] ?? []).toString("utf8")).toBe("\x1b[2Jhi");
		socket.close();
	});

	test("ignores input and resize before init", async () => {
		const host = recordingHost();
		const { wsUrl, origin } = startServer(host);
		const socket = await openSocket(wsUrl, { headers: { origin } });
		socket.send(new TextEncoder().encode("early"));
		socket.send(JSON.stringify({ type: "resize", cols: 90, rows: 30 }));
		socket.send(JSON.stringify({ type: "init", cols: 100, rows: 30 }));
		await waitFor(() => host.log.attached.length === 1);
		expect(host.log.inputs.length).toBe(0);
		expect(host.log.resizes.length).toBe(0);
		socket.close();
	});

	test("clamps init geometry and treats repeat init as resize", async () => {
		const host = recordingHost();
		const { wsUrl, origin } = startServer(host);
		const socket = await openSocket(wsUrl, { headers: { origin } });
		socket.send(JSON.stringify({ type: "init", cols: 10_000, rows: 1 }));
		await waitFor(() => host.log.attached.length === 1);
		expect(host.log.attached[0]).toMatchObject({ cols: 500, rows: 5 });
		socket.send(JSON.stringify({ type: "init", cols: 80, rows: 24 }));
		await waitFor(() => host.log.resizes.length === 1);
		expect(host.log.attached.length).toBe(1);
		socket.close();
	});

	test("a newer client replaces the active one with close code 4001", async () => {
		const host = recordingHost();
		const { wsUrl, origin } = startServer(host);
		const first = await openSocket(wsUrl, { headers: { origin } });
		first.send(JSON.stringify({ type: "init", cols: 80, rows: 24 }));
		await waitFor(() => host.log.attached.length === 1);

		const firstClosed = nextClose(first);
		const second = await openSocket(wsUrl, { headers: { origin } });
		const closeEvent = await firstClosed;
		expect(closeEvent.code).toBe(4001);
		await waitFor(() => host.log.detached.length === 1);

		second.send(JSON.stringify({ type: "init", cols: 100, rows: 40 }));
		await waitFor(() => host.log.attached.length === 2);
		second.close();
	});
});
