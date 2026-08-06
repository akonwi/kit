/** Bounded smoke test. Deliberately makes no prompt/model request. */
import { type Subprocess, spawn } from "bun";

type RpcRecord = Record<string, unknown>;
const HOST = "127.0.0.1";
const PORT = Number.parseInt(process.env.PORT ?? "44173", 10);
const BASE = `http://${HOST}:${PORT}`;
const WS_BASE = `ws://${HOST}:${PORT}/ws`;
const SERVER = new URL("./server.ts", import.meta.url).pathname;
const TIMEOUT_MS = 15_000;

function bounded<T>(
	promise: Promise<T>,
	label: string,
	ms = TIMEOUT_MS,
): Promise<T> {
	return Promise.race([
		promise,
		Bun.sleep(ms).then(() => {
			throw new Error(`${label} timed out after ${ms}ms`);
		}),
	]);
}

async function waitReady(proc: Subprocess): Promise<void> {
	await bounded(
		(async () => {
			for (;;) {
				if (proc.exitCode !== null)
					throw new Error(`server exited during startup (${proc.exitCode})`);
				try {
					const response = await fetch(`${BASE}/healthz`);
					if (response.ok) return;
				} catch {}
				await Bun.sleep(100);
			}
		})(),
		"server readiness",
	);
}

function connect(url: string): Promise<WebSocket> {
	return bounded(
		new Promise((resolve, reject) => {
			const ws = new WebSocket(url);
			ws.addEventListener("open", () => resolve(ws), { once: true });
			ws.addEventListener(
				"error",
				() => reject(new Error("WebSocket rejected")),
				{ once: true },
			);
		}),
		"WebSocket connect",
		5_000,
	);
}

async function expectRejected(url: string, label: string): Promise<void> {
	let ws: WebSocket | null = null;
	try {
		ws = await connect(url);
		throw new Error(`${label} unexpectedly connected`);
	} catch (error) {
		if (error instanceof Error && error.message.includes("unexpectedly"))
			throw error;
	} finally {
		ws?.close();
	}
}

function nextMessage(
	ws: WebSocket,
	predicate: (record: RpcRecord) => boolean,
): Promise<RpcRecord> {
	return bounded(
		new Promise((resolve) => {
			const handler = (event: MessageEvent) => {
				if (typeof event.data !== "string") return;
				try {
					const record = JSON.parse(event.data) as RpcRecord;
					if (predicate(record)) {
						ws.removeEventListener("message", handler);
						resolve(record);
					}
				} catch {}
			};
			ws.addEventListener("message", handler);
		}),
		"protocol response",
	);
}

function waitClosed(ws: WebSocket): Promise<void> {
	if (ws.readyState === WebSocket.CLOSED) return Promise.resolve();
	return bounded(
		new Promise((resolve) =>
			ws.addEventListener("close", () => resolve(), { once: true }),
		),
		"WebSocket close",
		5_000,
	);
}

async function stop(proc: Subprocess): Promise<void> {
	if (proc.exitCode !== null) return;
	proc.kill("SIGTERM");
	const exited = await Promise.race([
		proc.exited.then(() => true),
		Bun.sleep(6_000).then(() => false),
	]);
	if (!exited) {
		proc.kill("SIGKILL");
		await bounded(proc.exited, "forced server shutdown", 2_000);
		throw new Error("server did not complete bounded SIGTERM shutdown");
	}
}

let proc: Subprocess | null = null;
let ws: WebSocket | null = null;
try {
	proc = spawn({
		cmd: [process.execPath, SERVER],
		cwd: new URL("../..", import.meta.url).pathname,
		stdin: "ignore",
		stdout: "ignore",
		stderr: "inherit",
		env: { ...process.env, HOST, PORT: String(PORT) },
	});
	await waitReady(proc);

	const health = await bounded(fetch(`${BASE}/healthz`), "health request");
	if (!health.ok || ((await health.json()) as RpcRecord).ok !== true)
		throw new Error("invalid /healthz response");
	const page = await bounded(fetch(`${BASE}/`), "static page request");
	const html = await page.text();
	if (!page.ok || !html.includes("Kit RPC Bridge") || !html.includes("/app.js"))
		throw new Error("invalid static page");
	const token = /name="kit-rpc-token" content="([^"]+)"/.exec(html)?.[1];
	if (!token || token === "__KIT_RPC_TOKEN__")
		throw new Error("page did not contain a capability token");

	await expectRejected(WS_BASE, "tokenless client");
	ws = await connect(`${WS_BASE}?token=${encodeURIComponent(token)}`);
	await expectRejected(
		`${WS_BASE}?token=${encodeURIComponent(token)}`,
		"second client",
	);

	const stateId = "smoke-get-state";
	const stateResponse = nextMessage(
		ws,
		(record) => record.type === "response" && record.id === stateId,
	);
	ws.send(JSON.stringify({ id: stateId, type: "get_state" }));
	const state = await stateResponse;
	if (state.success !== true || state.command !== "get_state")
		throw new Error("get_state failed");

	const malformedResponse = nextMessage(
		ws,
		(record) => record.type === "bridge.error",
	);
	ws.send('{"truly": malformed');
	const malformed = await malformedResponse;
	if (typeof malformed.error !== "string")
		throw new Error("malformed input did not return bridge.error");

	ws.close();
	await waitClosed(ws);
	ws = await connect(`${WS_BASE}?token=${encodeURIComponent(token)}`);
	const reconnectId = "smoke-reconnect-state";
	const reconnectResponse = nextMessage(
		ws,
		(record) => record.type === "response" && record.id === reconnectId,
	);
	ws.send(JSON.stringify({ id: reconnectId, type: "get_state" }));
	if ((await reconnectResponse).success !== true)
		throw new Error("reconnected get_state failed");
	ws.close();
	await waitClosed(ws);
	ws = null;

	await stop(proc);
	proc = null;
	console.log(
		"OK: health, static/token auth, single client, get_state, malformed input, reconnect, and SIGTERM shutdown",
	);
} finally {
	ws?.close();
	if (proc) await stop(proc);
}
