import type { Server, ServerWebSocket } from "bun";
import {
	MAX_REMOTE_ATTACHMENT_BYTES,
	RemoteAttachmentError,
	type RemoteAttachmentStore,
} from "./remote-attachment-store";
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
	attachments?: RemoteAttachmentStore;
};

type WebSocketData = Record<string, never>;

const MAX_MULTIPART_BODY_BYTES = MAX_REMOTE_ATTACHMENT_BYTES + 1024 * 1024;
const MAX_CONCURRENT_UPLOADS = 4;

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
	private activeUploads = 0;
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
			maxRequestBodySize: MAX_MULTIPART_BODY_BYTES,
			fetch: async (request, bunServer) => {
				const url = new URL(request.url);
				if (url.pathname === "/api/health") {
					return Response.json({
						ok: true,
						mode: "web",
						clients: this.clients.size,
					});
				}
				if (
					url.pathname === "/api/attachments" ||
					url.pathname.startsWith("/api/attachments/")
				) {
					return this.handleAttachmentRequest(request, url);
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
			const status = socket.send(JSON.stringify(this.projectRecord(record)));
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
		for (const client of [...this.clients]) this.send(client, record);
	}

	private async handleAttachmentRequest(
		request: Request,
		url: URL,
	): Promise<Response> {
		if (!this.isAllowedHttpRequest(request, url)) {
			return new Response("Origin or host not allowed", { status: 403 });
		}
		const corsHeaders = this.corsHeaders(request);
		if (request.method === "OPTIONS") {
			return new Response(null, {
				status: 204,
				headers: {
					...corsHeaders,
					"access-control-allow-methods": "GET, POST, DELETE, OPTIONS",
				},
			});
		}
		if (!this.options.attachments) {
			return Response.json(
				{ error: "Attachments are unavailable" },
				{ status: 404, headers: corsHeaders },
			);
		}

		if (url.pathname === "/api/attachments" && request.method === "POST") {
			const contentLength = Number(request.headers.get("content-length"));
			if (
				Number.isFinite(contentLength) &&
				contentLength > MAX_MULTIPART_BODY_BYTES
			) {
				return Response.json(
					{ error: "Upload exceeds the request size limit" },
					{ status: 413, headers: corsHeaders },
				);
			}
			if (this.activeUploads >= MAX_CONCURRENT_UPLOADS) {
				return Response.json(
					{ error: "Too many concurrent uploads" },
					{ status: 429, headers: corsHeaders },
				);
			}
			this.activeUploads += 1;
			try {
				const form = await request.formData();
				const files = form.getAll("file");
				if (files.length !== 1 || !(files[0] instanceof File)) {
					throw new RemoteAttachmentError(
						'Multipart upload requires exactly one "file" field',
						400,
					);
				}
				const attachment = await this.options.attachments.add(files[0]);
				return Response.json(
					{ attachment },
					{ status: 201, headers: corsHeaders },
				);
			} catch (error) {
				const status =
					error instanceof RemoteAttachmentError ? error.status : 400;
				return Response.json(
					{ error: parseError(error) },
					{ status, headers: corsHeaders },
				);
			} finally {
				this.activeUploads -= 1;
			}
		}

		const attachmentId = url.pathname.slice("/api/attachments/".length);
		if (
			request.method === "GET" &&
			attachmentId &&
			!attachmentId.includes("/")
		) {
			const download = this.options.attachments.download(attachmentId);
			if (!download) {
				return Response.json(
					{ error: "Attachment not found" },
					{ status: 404, headers: corsHeaders },
				);
			}
			const isImage = download.metadata.kind === "image";
			return new Response(download.bytes, {
				headers: {
					...corsHeaders,
					"cache-control": "private, no-store",
					"content-disposition": isImage ? "inline" : "attachment",
					"content-type": isImage
						? download.metadata.mimeType
						: "application/octet-stream",
					"x-content-type-options": "nosniff",
				},
			});
		}
		if (
			request.method === "DELETE" &&
			attachmentId &&
			!attachmentId.includes("/")
		) {
			if (!this.options.attachments.remove(attachmentId)) {
				return Response.json(
					{ error: "Attachment not found" },
					{ status: 404, headers: corsHeaders },
				);
			}
			return new Response(null, { status: 204, headers: corsHeaders });
		}

		return new Response("Method not allowed", {
			status: 405,
			headers: { ...corsHeaders, allow: "GET, POST, DELETE, OPTIONS" },
		});
	}

	private isAllowedWebSocketRequest(request: Request, url: URL): boolean {
		return (
			this.allowedHosts().has(url.host.toLowerCase()) &&
			this.isAllowedOrigin(
				request.headers.get("origin"),
				url,
				this.options.allowOriginless === true,
			)
		);
	}

	private isAllowedHttpRequest(request: Request, url: URL): boolean {
		return (
			this.allowedHosts().has(url.host.toLowerCase()) &&
			this.isAllowedOrigin(
				request.headers.get("origin"),
				url,
				request.method === "GET" || this.options.allowOriginless === true,
			)
		);
	}

	private isAllowedOrigin(
		origin: string | null,
		url: URL,
		allowOriginless: boolean,
	): boolean {
		if (!origin) return allowOriginless;
		try {
			return this.allowedOrigins(url).has(new URL(origin).origin.toLowerCase());
		} catch {
			return false;
		}
	}

	private allowedOrigins(url: URL): Set<string> {
		const configuredOrigins = this.options.allowedOrigins ?? [];
		const origins = new Set(
			configuredOrigins.map((value) => new URL(value).origin.toLowerCase()),
		);
		if (configuredOrigins.length === 0) {
			origins.add(url.origin.toLowerCase());
		}
		return origins;
	}

	private corsHeaders(request: Request): Record<string, string> {
		const origin = request.headers.get("origin");
		return origin
			? {
					"access-control-allow-origin": new URL(origin).origin,
					vary: "origin",
				}
			: {};
	}

	private projectRecord(record: unknown): unknown {
		if (isRecord(record) && record.type === "agent_end") {
			const projected = { ...record };
			delete projected.messages;
			return this.projectValue(projected);
		}
		return this.projectValue(record);
	}

	private projectValue(value: unknown): unknown {
		if (Array.isArray(value)) {
			return value.map((item) => this.projectValue(item));
		}
		if (!isRecord(value)) return value;
		if (value.type === "image" && typeof value.data === "string") {
			const projected: Record<string, unknown> = {
				...value,
				dataOmitted: true,
			};
			delete projected.data;
			delete projected.sourcePath;
			return projected;
		}
		return Object.fromEntries(
			Object.entries(value).map(([key, item]) => [
				key,
				this.projectValue(item),
			]),
		);
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
