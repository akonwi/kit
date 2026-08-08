import type { Server, ServerWebSocket } from "bun";
import type {
	RpcCommand,
	RpcEventListener,
	RpcWriter,
} from "./rpc-session-host";

export type WebRpcHost = {
	subscribe(listener: RpcEventListener): () => void;
	handleCommand(command: RpcCommand, respond: RpcWriter): Promise<void>;
	connectClient?(listener: RpcEventListener): () => void;
};

export type WebRpcServerOptions = {
	hostname?: string;
	port?: number;
	allowedHosts?: string[];
	allowedOrigins?: string[];
	allowOriginless?: boolean;
};

type WebSocketData = Record<string, never>;

const PLACEHOLDER_HTML = `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Kit web mode</title>
</head>
<body>
	<main>
		<h1>Kit web mode</h1>
		<p>The RPC server is running. The browser client is coming next.</p>
	</main>
</body>
</html>`;

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseCommand(message: string | Buffer): RpcCommand {
	const parsed: unknown = JSON.parse(
		typeof message === "string" ? message : message.toString("utf8"),
	);
	if (!isRecord(parsed) || typeof parsed.type !== "string") {
		throw new Error("Command must be an object with a string type");
	}
	return parsed as RpcCommand;
}

function parseError(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

export class WebRpcServer {
	private server: Server<WebSocketData> | null = null;
	private unsubscribeHost: (() => void) | null = null;
	private readonly clients = new Set<ServerWebSocket<WebSocketData>>();
	private readonly clientDisposers = new Map<
		ServerWebSocket<WebSocketData>,
		() => void
	>();

	constructor(
		private readonly rpcHost: WebRpcHost,
		private readonly options: WebRpcServerOptions = {},
	) {}

	start(): { hostname: string; port: number; url: string } {
		if (this.server) throw new Error("Web RPC server is already running");
		this.unsubscribeHost = this.rpcHost.subscribe((record) => {
			this.broadcast(record);
		});

		const server = Bun.serve<WebSocketData>({
			hostname: this.options.hostname ?? "127.0.0.1",
			port: this.options.port ?? 4782,
			fetch: (request, bunServer) => {
				const url = new URL(request.url);
				if (url.pathname === "/api/health") {
					return Response.json({
						ok: true,
						mode: "web",
						clients: this.clients.size,
					});
				}
				if (url.pathname === "/api/rpc") {
					if (!this.isAllowedWebSocketRequest(request, url)) {
						return new Response("Origin or host not allowed", { status: 403 });
					}
					if (bunServer.upgrade(request, { data: {} })) return undefined;
					return new Response("WebSocket upgrade required", { status: 426 });
				}
				if (url.pathname === "/") {
					return new Response(PLACEHOLDER_HTML, {
						headers: { "content-type": "text/html; charset=utf-8" },
					});
				}
				return new Response("Not found", { status: 404 });
			},
			websocket: {
				maxPayloadLength: 1024 * 1024,
				backpressureLimit: 16 * 1024 * 1024,
				closeOnBackpressureLimit: true,
				open: (socket) => {
					this.clients.add(socket);
					const disconnect = this.rpcHost.connectClient?.((record) => {
						this.send(socket, record);
					});
					if (disconnect && this.clients.has(socket)) {
						this.clientDisposers.set(socket, disconnect);
					} else {
						disconnect?.();
					}
				},
				message: (socket, message) => {
					let command: RpcCommand;
					try {
						command = parseCommand(message);
					} catch (error) {
						this.send(socket, {
							type: "response",
							command: "parse",
							success: false,
							error: `Failed to parse command: ${parseError(error)}`,
						});
						return;
					}
					void this.rpcHost.handleCommand(command, async (record) => {
						this.send(socket, record);
					});
				},
				close: (socket) => {
					this.removeClient(socket);
				},
			},
		});
		this.server = server;
		const hostname = server.hostname ?? this.options.hostname ?? "127.0.0.1";
		const port = server.port;
		if (port === undefined)
			throw new Error("Web RPC server did not bind a port");
		return { hostname, port, url: server.url.origin };
	}

	async stop(): Promise<void> {
		this.unsubscribeHost?.();
		this.unsubscribeHost = null;
		for (const client of [...this.clients]) this.removeClient(client);
		const server = this.server;
		this.server = null;
		await server?.stop(true);
	}

	private send(socket: ServerWebSocket<WebSocketData>, record: unknown): void {
		if (socket.readyState !== WebSocket.OPEN) {
			this.removeClient(socket);
			return;
		}
		try {
			const status = socket.send(JSON.stringify(record));
			if (status === 0) this.removeClient(socket, true);
		} catch (error) {
			this.removeClient(socket, true);
			console.error(
				`WebSocket send failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		}
	}

	private removeClient(
		socket: ServerWebSocket<WebSocketData>,
		terminate = false,
	): void {
		this.clients.delete(socket);
		this.clientDisposers.get(socket)?.();
		this.clientDisposers.delete(socket);
		if (terminate) socket.terminate();
	}

	private broadcast(record: unknown): void {
		const projected = this.projectEvent(record);
		for (const client of [...this.clients]) this.send(client, projected);
	}

	private isAllowedWebSocketRequest(request: Request, url: URL): boolean {
		if (!this.allowedHosts().has(url.host.toLowerCase())) return false;
		const origin = request.headers.get("origin");
		if (!origin) return this.options.allowOriginless === true;
		try {
			const requestOrigin = new URL(origin).origin.toLowerCase();
			const configuredOrigins = this.options.allowedOrigins ?? [];
			const allowedOrigins = new Set(
				configuredOrigins.map((value) => new URL(value).origin.toLowerCase()),
			);
			if (configuredOrigins.length === 0) {
				allowedOrigins.add(url.origin.toLowerCase());
			}
			return allowedOrigins.has(requestOrigin);
		} catch {
			return false;
		}
	}

	private projectEvent(record: unknown): unknown {
		if (!isRecord(record) || record.type !== "agent_end") return record;
		const projected = { ...record };
		delete projected.messages;
		return projected;
	}

	private allowedHosts(): Set<string> {
		const hostname =
			this.server?.hostname ?? this.options.hostname ?? "127.0.0.1";
		const port = this.server?.port ?? this.options.port ?? 4782;
		const hosts = new Set(
			(this.options.allowedHosts ?? []).map((host) => host.toLowerCase()),
		);
		hosts.add(`${hostname}:${port}`.toLowerCase());
		if (hostname === "127.0.0.1" || hostname === "::1") {
			hosts.add(`localhost:${port}`);
			hosts.add(`127.0.0.1:${port}`);
			hosts.add(`[::1]:${port}`);
		}
		return hosts;
	}
}
