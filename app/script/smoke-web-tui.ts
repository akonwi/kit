#!/usr/bin/env bun
/**
 * Smoke test for the experimental browser TUI mode (ghostty-web experiment).
 *
 * Boots `kit --web-tui` with an ephemeral session, then acts as
 * the browser terminal over the real WebSocket protocol: init, streamed ANSI
 * output, keyboard input, resize, and reconnect. Asserts the hosted OpenTUI
 * application produces genuine terminal frames (alternate screen, repaints)
 * rather than static output.
 *
 * Run from app/: bun run script/smoke-web-tui.ts
 * Against the compiled binary: KIT_WEB_TUI_SMOKE_BIN=dist/kit bun run script/smoke-web-tui.ts
 */

import path from "node:path";
import { WEB_TUI_PROTOCOL_VERSION } from "../src/web-tui/browser-actions";

const dir = path.resolve(import.meta.dirname, "..");
const port = 4000 + Math.floor(Math.random() * 2000);
const origin = `http://127.0.0.1:${port}`;

const WebSocketWithOptions = WebSocket as unknown as new (
	url: string,
	options?: Bun.WebSocketOptions,
) => WebSocket;

function fail(message: string): never {
	throw new Error(`SMOKE FAIL: ${message}`);
}

type Connection = {
	socket: WebSocket;
	received: () => string;
	waitForOutput: (needle: string, timeoutMs?: number) => Promise<void>;
	bytesSeen: () => number;
	close: () => void;
};

async function connect(): Promise<Connection> {
	const socket = new WebSocketWithOptions(`ws://127.0.0.1:${port}/api/tui`, {
		headers: { origin },
	});
	socket.binaryType = "arraybuffer";
	let received = "";
	let bytes = 0;
	const decoder = new TextDecoder();
	socket.addEventListener("message", (event) => {
		if (event.data instanceof ArrayBuffer) {
			bytes += event.data.byteLength;
			received += decoder.decode(new Uint8Array(event.data), { stream: true });
		}
	});
	await new Promise<void>((resolve, reject) => {
		socket.addEventListener("open", () => resolve(), { once: true });
		socket.addEventListener("error", () => reject(new Error("ws error")), {
			once: true,
		});
	});
	return {
		socket,
		received: () => received,
		bytesSeen: () => bytes,
		waitForOutput: async (needle, timeoutMs = 20_000) => {
			const deadline = Date.now() + timeoutMs;
			while (!received.includes(needle)) {
				if (Date.now() > deadline) {
					fail(
						`timed out waiting for ${JSON.stringify(needle)}; received ${bytes} bytes`,
					);
				}
				await Bun.sleep(50);
			}
		},
		close: () => socket.close(),
	};
}

console.log(`Starting kit --web-tui on port ${port}...`);
const smokeBinary = process.env.KIT_WEB_TUI_SMOKE_BIN;
const spawnServer = () =>
	Bun.spawn({
		cmd: [
			...(smokeBinary
				? [path.resolve(dir, smokeBinary)]
				: ["bun", "--preload=@opentui/solid/preload", "src/app/main.tsx"]),
			"--web-tui",
			"--no-session",
			"--port",
			String(port),
			"--public-url",
			origin,
		],
		cwd: dir,
		env: { ...process.env, KIT_DEBUG_SHUTDOWN: "1" },
		stdout: "pipe",
		stderr: "pipe",
	});

async function waitForHealth(): Promise<void> {
	const deadline = Date.now() + 20_000;
	for (;;) {
		try {
			const health = await fetch(`${origin}/api/health`);
			const body = (await health.json()) as { mode?: string };
			if (body.mode === "web-tui") return;
		} catch {}
		if (Date.now() > deadline) fail("server did not become healthy");
		await Bun.sleep(100);
	}
}

async function assertPortReusable(): Promise<void> {
	const probe = Bun.serve({
		hostname: "127.0.0.1",
		port,
		fetch: () => new Response("ok"),
	});
	await probe.stop(true);
}

const server = spawnServer();
const serverExited = server.exited.then((code) => code);
let terminationServer: ReturnType<typeof spawnServer> | null = null;

try {
	// Wait for the health endpoint.
	await waitForHealth();
	console.log("✓ health endpoint reports web-tui mode");

	// Document + assets sanity.
	const doc = await fetch(`${origin}/`);
	if (doc.status !== 200) fail(`document status ${doc.status}`);
	const csp = doc.headers.get("content-security-policy") ?? "";
	if (!csp.includes("'wasm-unsafe-eval'")) fail("CSP missing wasm-unsafe-eval");
	const clientJs = await fetch(`${origin}/assets/tui-client.js`);
	const clientSource = await clientJs.text();
	if (clientSource.length < 100_000 || !clientSource.includes("ghostty-web")) {
		fail("tui client bundle missing ghostty-web");
	}
	const wasm = await fetch(`${origin}/assets/ghostty-vt.wasm`);
	if ((await wasm.arrayBuffer()).byteLength < 100_000) {
		fail("ghostty wasm asset looks truncated");
	}
	console.log("✓ document, client bundle, and wasm asset served");

	// First client: app boots lazily, enters the alternate screen, paints.
	const first = await connect();
	first.socket.send(
		JSON.stringify({
			type: "init",
			cols: 100,
			rows: 30,
			protocolVersion: WEB_TUI_PROTOCOL_VERSION,
		}),
	);
	await first.waitForOutput("\x1b[?1049h", 30_000);
	console.log("✓ OpenTUI entered the alternate screen after first init");
	const paintDeadline = Date.now() + 30_000;
	while (first.bytesSeen() < 2_000) {
		if (Date.now() > paintDeadline) fail("no substantial frame output");
		await Bun.sleep(100);
	}
	console.log(`✓ initial frames streamed (${first.bytesSeen()} bytes)`);

	// Keyboard input reaches the app: typing into the composer repaints.
	const beforeTyping = first.bytesSeen();
	first.socket.send(new TextEncoder().encode("hello from ghostty-web"));
	{
		const deadline = Date.now() + 10_000;
		while (first.bytesSeen() <= beforeTyping) {
			if (Date.now() > deadline) fail("typing produced no repaint");
			await Bun.sleep(50);
		}
	}
	console.log("✓ keyboard input produced output frames");

	// Resize triggers a reflow.
	const beforeResize = first.bytesSeen();
	first.socket.send(JSON.stringify({ type: "resize", cols: 80, rows: 24 }));
	{
		const deadline = Date.now() + 10_000;
		while (first.bytesSeen() <= beforeResize) {
			if (Date.now() > deadline) fail("resize produced no repaint");
			await Bun.sleep(50);
		}
	}
	console.log("✓ resize produced a reflow");

	// Reconnect: a fresh client must receive terminal setup + a full repaint.
	first.close();
	await Bun.sleep(500);
	const second = await connect();
	second.socket.send(
		JSON.stringify({
			type: "init",
			cols: 90,
			rows: 28,
			protocolVersion: WEB_TUI_PROTOCOL_VERSION,
		}),
	);
	await second.waitForOutput("\x1b[?1049h", 15_000);
	{
		const deadline = Date.now() + 15_000;
		while (second.bytesSeen() < 2_000) {
			if (Date.now() > deadline) fail("reconnect produced no full repaint");
			await Bun.sleep(100);
		}
	}
	console.log(
		`✓ reconnect replayed terminal setup and repainted (${second.bytesSeen()} bytes)`,
	);

	// Orderly shutdown while a browser client remains attached.
	server.kill("SIGINT");
	const code = await Promise.race([
		serverExited,
		Bun.sleep(10_000).then(() => "timeout" as const),
	]);
	if (code === "timeout") fail("server did not exit after SIGINT");
	if (code !== 130) fail(`SIGINT exit code was ${code}, expected 130`);
	const interruptDiagnostics = await new Response(server.stderr).text();
	if (!interruptDiagnostics.includes("[kit] web TUI shutdown complete")) {
		fail("SIGINT process exited before reporting completed cleanup");
	}
	await assertPortReusable();
	console.log("✓ SIGINT exited with code 130 after releasing the server port");

	// SIGTERM before a browser attaches still owns and completes server cleanup.
	terminationServer = spawnServer();
	await waitForHealth();
	terminationServer.kill("SIGTERM");
	const terminationCode = await Promise.race([
		terminationServer.exited,
		Bun.sleep(10_000).then(() => "timeout" as const),
	]);
	if (terminationCode === "timeout") fail("server did not exit after SIGTERM");
	if (terminationCode !== 143) {
		fail(`SIGTERM exit code was ${terminationCode}, expected 143`);
	}
	const terminationDiagnostics = await new Response(
		terminationServer.stderr,
	).text();
	if (!terminationDiagnostics.includes("[kit] web TUI shutdown complete")) {
		fail("SIGTERM process exited before reporting completed cleanup");
	}
	await assertPortReusable();
	console.log("✓ SIGTERM exited with code 143 after releasing the server port");
	console.log("SMOKE PASS");
} finally {
	server.kill();
	terminationServer?.kill();
	await Promise.allSettled([
		server.exited,
		...(terminationServer ? [terminationServer.exited] : []),
	]);
}
