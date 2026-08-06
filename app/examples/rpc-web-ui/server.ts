/** Single-client browser bridge for Kit's JSONL RPC mode. */

import { join, resolve } from "node:path";
import { type Subprocess, spawn } from "bun";

type RecordValue = Record<string, unknown>;
type SocketData = { id: number };

const HOST = process.env.HOST ?? "127.0.0.1";
const PORT = Number.parseInt(process.env.PORT ?? "4173", 10);
const APP_DIR = resolve(import.meta.dir, "../..");
const PUBLIC_DIR = join(import.meta.dir, "public");
const TOKEN = crypto.randomUUID();
const MAX_WS_PAYLOAD = 256 * 1024;
const MAX_CHILD_LINE = 1024 * 1024;
const STOP_TIMEOUT_MS = 2_000;
const command = [
	process.execPath,
	"--preload=@opentui/solid/preload",
	"src/app/main.tsx",
	"--mode",
	"rpc",
	"--no-session",
];

function log(message: string): void {
	process.stderr.write(`[rpc-web-ui] ${message}\n`);
}

function bridgeError(error: string): RecordValue {
	return { type: "bridge.error", error };
}

let child: Subprocess | null = null;
let childWrite = Promise.resolve();
let activeSocket: {
	send(data: string): number;
	close(code?: number, reason?: string): void;
} | null = null;
let reservedClient = false;
let nextClientId = 1;
let shuttingDown = false;

function send(record: RecordValue): void {
	if (!activeSocket) return;
	try {
		activeSocket.send(JSON.stringify(record));
	} catch {
		// The close callback releases the active-client slot.
	}
}

function handleChildLine(line: string): void {
	if (!line) return;
	try {
		const value: unknown = JSON.parse(line);
		if (!value || typeof value !== "object" || Array.isArray(value))
			throw new Error("not an object");
		send(value as RecordValue);
	} catch (error) {
		const reason =
			error instanceof Error ? error.message.slice(0, 160) : "parse failed";
		log(`malformed child JSONL (${line.length} bytes): ${reason}`);
		send(bridgeError("child emitted malformed protocol output"));
	}
}

async function readChildStdout(proc: Subprocess): Promise<void> {
	const reader = (proc.stdout as ReadableStream<Uint8Array>).getReader();
	const decoder = new TextDecoder();
	let buffer = "";
	try {
		for (;;) {
			const { done, value } = await reader.read();
			if (done) break;
			buffer += decoder.decode(value, { stream: true });
			if (buffer.length > MAX_CHILD_LINE && !buffer.includes("\n")) {
				log(
					`child JSONL line exceeded ${MAX_CHILD_LINE} bytes; terminating child`,
				);
				send(bridgeError("child protocol record exceeded bridge limit"));
				proc.kill("SIGTERM");
				return;
			}
			let newline = buffer.indexOf("\n");
			while (newline >= 0) {
				const line = buffer.slice(0, newline).replace(/\r$/, "");
				buffer = buffer.slice(newline + 1);
				if (line.length > MAX_CHILD_LINE) {
					log(`discarded oversized child JSONL record (${line.length} bytes)`);
					send(bridgeError("child protocol record exceeded bridge limit"));
				} else handleChildLine(line);
				newline = buffer.indexOf("\n");
			}
		}
		buffer += decoder.decode();
		if (buffer.length <= MAX_CHILD_LINE)
			handleChildLine(buffer.replace(/\r$/, ""));
	} catch (error) {
		if (!shuttingDown)
			log(
				`child stdout failed: ${error instanceof Error ? error.message : String(error)}`,
			);
	}
}

function startChild(): void {
	log(`spawning ${command.join(" ")} (cwd=${APP_DIR})`);
	const proc = spawn({
		cmd: command,
		cwd: APP_DIR,
		stdin: "pipe",
		stdout: "pipe",
		stderr: "inherit",
	});
	child = proc;
	void readChildStdout(proc);
	void proc.exited.then((code) => {
		if (child === proc) child = null;
		log(`child exited code=${code}`);
		if (!shuttingDown) send({ type: "bridge.child_exit", code });
	});
}

function writeChild(record: RecordValue): void {
	childWrite = childWrite
		.then(async () => {
			const proc = child;
			if (!proc) throw new Error("child is not running");
			await proc.stdin.write(`${JSON.stringify(record)}\n`);
		})
		.catch((error) => {
			log(
				`child stdin write failed: ${error instanceof Error ? error.message : String(error)}`,
			);
			send(bridgeError("unable to write to Kit"));
		});
}

async function staticResponse(pathname: string): Promise<Response> {
	const names: Record<string, string> = {
		"/app.js": "app.js",
		"/styles.css": "styles.css",
	};
	if (pathname === "/" || pathname === "/index.html") {
		const html = await Bun.file(join(PUBLIC_DIR, "index.html")).text();
		return new Response(html.replace("__KIT_RPC_TOKEN__", TOKEN), {
			headers: {
				"content-type": "text/html; charset=utf-8",
				"cache-control": "no-store",
			},
		});
	}
	const name = names[pathname];
	if (!name) return new Response("not found", { status: 404 });
	return new Response(Bun.file(join(PUBLIC_DIR, name)), {
		headers: {
			"content-type": name.endsWith(".css")
				? "text/css; charset=utf-8"
				: "application/javascript; charset=utf-8",
			"cache-control": "no-store",
		},
	});
}

const server = Bun.serve<SocketData>({
	hostname: HOST,
	port: PORT,
	websocket: {
		maxPayloadLength: MAX_WS_PAYLOAD,
		open(ws) {
			activeSocket = ws;
			reservedClient = false;
			ws.send(
				JSON.stringify({
					type: "bridge.hello",
					clientId: ws.data.id,
					pid: child?.pid ?? null,
				}),
			);
		},
		message(ws, message) {
			if (ws !== activeSocket) return;
			if (
				typeof message !== "string" ||
				message.includes("\n") ||
				message.includes("\r")
			) {
				send(bridgeError("message must be one JSON object"));
				return;
			}
			try {
				const value: unknown = JSON.parse(message);
				if (!value || typeof value !== "object" || Array.isArray(value))
					throw new Error();
				const record = value as RecordValue;
				if (record.type === "bridge.ping") send({ type: "bridge.pong" });
				else writeChild(record);
			} catch {
				send(bridgeError("message must be one valid JSON object"));
			}
		},
		close(ws) {
			if (ws === activeSocket) activeSocket = null;
		},
	},
	async fetch(request, bunServer) {
		const url = new URL(request.url);
		if (url.pathname === "/healthz") {
			return Response.json({ ok: true, child: child ? "running" : "exited" });
		}
		if (url.pathname === "/ws") {
			if (url.searchParams.get("token") !== TOKEN)
				return new Response("forbidden", { status: 403 });
			if (activeSocket || reservedClient)
				return new Response("client already connected", { status: 409 });
			reservedClient = true;
			if (bunServer.upgrade(request, { data: { id: nextClientId++ } })) return;
			reservedClient = false;
			return new Response("upgrade required", { status: 426 });
		}
		return staticResponse(url.pathname);
	},
});

startChild();
log(`listening on http://${server.hostname}:${server.port}`);

async function stopChild(): Promise<void> {
	const proc = child;
	if (!proc) return;
	try {
		proc.stdin.end();
	} catch {}
	try {
		proc.kill("SIGTERM");
	} catch {}
	const exited = proc.exited.then(() => true).catch(() => true);
	if (
		!(await Promise.race([
			exited,
			Bun.sleep(STOP_TIMEOUT_MS).then(() => false),
		]))
	) {
		log("child did not stop after SIGTERM; sending SIGKILL");
		try {
			proc.kill("SIGKILL");
		} catch {}
		await Promise.race([exited, Bun.sleep(STOP_TIMEOUT_MS)]);
	}
}

async function shutdown(signal: "SIGINT" | "SIGTERM"): Promise<void> {
	if (shuttingDown) return;
	shuttingDown = true;
	log(`received ${signal}; shutting down`);
	server.stop(true);
	activeSocket?.close(1001, "server shutting down");
	await stopChild();
	process.exit(signal === "SIGINT" ? 130 : 143);
}

process.once("SIGINT", () => void shutdown("SIGINT"));
process.once("SIGTERM", () => void shutdown("SIGTERM"));
