/**
 * Experimental browser-TUI server (ghostty-web experiment).
 *
 * Serves a same-origin ghostty-web terminal page and bridges one WebSocket client to
 * the in-process OpenTUI application hosted by `WebTuiBridge`. Request
 * security intentionally mirrors `WebRpcServer`: Host allowlisting, Origin
 * validation, optional Basic authentication, restrictive CSP, and
 * first-party-only asset delivery. The only CSP delta from the SPA document is
 * `'wasm-unsafe-eval'`, required to instantiate the Ghostty terminal core.
 */

import { createHash, timingSafeEqual } from "node:crypto";
import jetbrainsMonoItalic from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-italic.woff2" with {
	type: "file",
};
import jetbrainsMonoNormal from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-normal.woff2" with {
	type: "file",
};
import type { Server, ServerWebSocket } from "bun";
import ghosttyWasm from "ghostty-web/ghostty-vt.wasm" with { type: "file" };
import tuiHtml from "../web-tui/index.html" with { type: "text" };
// @ts-expect-error: Bun's text loader embeds non-TypeScript browser assets.
import tuiCss from "../web-tui/tui.css" with { type: "text" };
import { clampTuiSize } from "./web-tui-bridge";

export type WebTuiClient = {
	send(bytes: Uint8Array): void;
};

/** Terminal host driven by the server; implemented by WebTuiBridge. */
export type WebTuiHost = {
	attach(client: WebTuiClient, cols: number, rows: number): void;
	detach(client: WebTuiClient): void;
	input(bytes: Uint8Array): boolean;
	resize(cols: number, rows: number): void;
};

export type WebTuiBasicAuthCredentials = {
	username: string;
	password: string;
};

export type WebTuiServerOptions = {
	hostname?: string;
	port?: number;
	allowedHosts?: string[];
	allowedOrigins?: string[];
	allowOriginless?: boolean;
	basicAuth?: WebTuiBasicAuthCredentials;
};

type WebSocketData = {
	client: WebTuiClient | null;
};

declare const __KIT_WEB_TUI_CLIENT_JS__: string | undefined;

let developmentTuiClient: Promise<string> | null = null;

function webTuiClientJavaScript(): Promise<string> {
	if (typeof __KIT_WEB_TUI_CLIENT_JS__ === "string") {
		return Promise.resolve(__KIT_WEB_TUI_CLIENT_JS__);
	}
	const developmentBuilderUrl = new URL(
		"../web-tui/build-tui-client.ts",
		import.meta.url,
	).href;
	developmentTuiClient ??= import(developmentBuilderUrl).then(
		({ buildWebTuiClient }: typeof import("../web-tui/build-tui-client")) =>
			buildWebTuiClient(),
	);
	return developmentTuiClient;
}

const TUI_ASSETS = new Map<
	string,
	{ body: string | Blob; contentType: string }
>([
	["/assets/tui.css", { body: tuiCss, contentType: "text/css; charset=utf-8" }],
	[
		"/assets/ghostty-vt.wasm",
		{
			body: Bun.file(new URL(ghosttyWasm, import.meta.url)),
			contentType: "application/wasm",
		},
	],
	[
		"/assets/jetbrains-mono-normal.woff2",
		{
			body: Bun.file(new URL(jetbrainsMonoNormal, import.meta.url)),
			contentType: "font/woff2",
		},
	],
	[
		"/assets/jetbrains-mono-italic.woff2",
		{
			body: Bun.file(new URL(jetbrainsMonoItalic, import.meta.url)),
			contentType: "font/woff2",
		},
	],
]);

function tuiDocumentHeaders(url: URL): HeadersInit {
	const webSocketOrigins = `ws://${url.host} wss://${url.host}`;
	return {
		"content-type": "text/html; charset=utf-8",
		"content-security-policy": [
			"default-src 'self'",
			"base-uri 'none'",
			`connect-src 'self' ${webSocketOrigins}`,
			"font-src 'self'",
			"form-action 'self'",
			"frame-ancestors 'none'",
			"img-src 'self' data:",
			"object-src 'none'",
			// wasm-unsafe-eval is required to instantiate the Ghostty VT core.
			"script-src 'self' 'wasm-unsafe-eval'",
			"style-src 'self'",
		].join("; "),
		"referrer-policy": "no-referrer",
		"x-content-type-options": "nosniff",
	};
}

function credentialDigest(value: string): Buffer {
	return createHash("sha256").update(value, "utf8").digest();
}

function normalizeOrigin(value: string): string | null {
	if (value === "null") return value;
	try {
		const origin = new URL(value).origin.toLowerCase();
		return origin === "null" ? null : origin;
	} catch {
		return null;
	}
}

function decodeBasicAuthorization(header: string | null): string | null {
	const match = header?.match(/^Basic\s+([A-Za-z0-9+/=]+)$/i);
	if (!match?.[1]) return null;
	try {
		return Buffer.from(match[1], "base64").toString("utf8");
	} catch {
		return null;
	}
}

type ControlMessage = { type: "init" | "resize"; cols: number; rows: number };

function parseControlMessage(message: string): ControlMessage | null {
	if (message.length > 256) return null;
	let parsed: unknown;
	try {
		parsed = JSON.parse(message);
	} catch {
		return null;
	}
	if (typeof parsed !== "object" || parsed === null) return null;
	const record = parsed as Record<string, unknown>;
	if (record.type !== "init" && record.type !== "resize") return null;
	if (
		typeof record.cols !== "number" ||
		typeof record.rows !== "number" ||
		!Number.isFinite(record.cols) ||
		!Number.isFinite(record.rows)
	) {
		return null;
	}
	const size = clampTuiSize(record.cols, record.rows);
	return { type: record.type, cols: size.cols, rows: size.rows };
}

export class WebTuiServer {
	private server: Server<WebSocketData> | null = null;
	private activeSocket: ServerWebSocket<WebSocketData> | null = null;
	private readonly clients = new Set<ServerWebSocket<WebSocketData>>();
	private readonly expectedBasicAuthDigest: Buffer | null;

	constructor(
		private readonly host: WebTuiHost,
		private readonly options: WebTuiServerOptions = {},
	) {
		this.expectedBasicAuthDigest = options.basicAuth
			? credentialDigest(
					`${options.basicAuth.username}:${options.basicAuth.password}`,
				)
			: null;
	}

	get clientCount(): number {
		return this.activeSocket ? 1 : 0;
	}

	start(): { hostname: string; port: number; url: string } {
		if (this.server) throw new Error("Web TUI server is already running");
		const server = Bun.serve<WebSocketData>({
			hostname: this.options.hostname ?? "127.0.0.1",
			port: this.options.port ?? 4783,
			fetch: async (request, bunServer) => {
				const url = new URL(request.url);
				if (!this.isAllowedHost(url.host)) {
					return new Response("Host not allowed", { status: 403 });
				}
				const isWebSocketRequest = url.pathname === "/api/tui";
				if (
					isWebSocketRequest &&
					!this.isAllowedWebSocketRequest(request, url)
				) {
					return new Response("Origin or host not allowed", { status: 403 });
				}
				if (!this.isAuthorized(request)) {
					return this.authenticationRequiredResponse();
				}
				if (url.pathname === "/assets/tui-client.js") {
					return new Response(await webTuiClientJavaScript(), {
						headers: {
							"cache-control": "no-cache",
							"content-type": "text/javascript; charset=utf-8",
							"x-content-type-options": "nosniff",
						},
					});
				}
				const asset = TUI_ASSETS.get(url.pathname);
				if (asset) {
					return new Response(asset.body, {
						headers: {
							"cache-control": "no-cache",
							"content-type": asset.contentType,
							"x-content-type-options": "nosniff",
						},
					});
				}
				if (url.pathname === "/api/health") {
					return Response.json({
						ok: true,
						mode: "web-tui",
						experimental: true,
						clients: this.clientCount,
					});
				}
				if (isWebSocketRequest) {
					if (bunServer.upgrade(request, { data: { client: null } })) {
						return undefined;
					}
					return new Response("WebSocket upgrade required", { status: 426 });
				}
				if (url.pathname === "/") {
					return new Response(tuiHtml as unknown as string, {
						headers: tuiDocumentHeaders(url),
					});
				}
				return new Response("Not found", { status: 404 });
			},
			websocket: {
				maxPayloadLength: 64 * 1024,
				backpressureLimit: 16 * 1024 * 1024,
				closeOnBackpressureLimit: true,
				open: (socket) => {
					this.clients.add(socket);
					// Single-terminal policy: a new connection replaces the old one.
					const previous = this.activeSocket;
					this.activeSocket = socket;
					if (previous) {
						this.releaseSocket(previous);
						previous.close(4001, "replaced by a newer client");
					}
				},
				message: (socket, message) => {
					if (this.activeSocket !== socket) return;
					if (typeof message === "string") {
						const control = parseControlMessage(message);
						if (!control) return;
						if (control.type === "init") {
							if (socket.data.client) {
								this.host.resize(control.cols, control.rows);
								return;
							}
							const client: WebTuiClient = {
								send: (bytes) => this.send(socket, bytes),
							};
							socket.data.client = client;
							this.host.attach(client, control.cols, control.rows);
							return;
						}
						if (socket.data.client) {
							this.host.resize(control.cols, control.rows);
						}
						return;
					}
					if (socket.data.client && !this.host.input(new Uint8Array(message))) {
						socket.close(1009, "terminal input buffer exceeded");
					}
				},
				close: (socket) => {
					this.clients.delete(socket);
					if (this.activeSocket === socket) this.activeSocket = null;
					this.releaseSocket(socket);
				},
			},
		});
		this.server = server;
		return {
			hostname: server.hostname ?? this.options.hostname ?? "127.0.0.1",
			port: server.port ?? this.options.port ?? 4783,
			url: server.url.toString(),
		};
	}

	async stop(): Promise<void> {
		this.activeSocket = null;
		for (const client of this.clients) {
			this.releaseSocket(client);
			client.terminate();
		}
		this.clients.clear();
		const server = this.server;
		this.server = null;
		if (server) {
			const stopped = server.stop(true).catch((error) => {
				console.error(
					`Web TUI server stop failed: ${error instanceof Error ? error.message : String(error)}`,
				);
			});
			// Bun may await a browser peer's close handshake despite force=true.
			await Promise.race([stopped, Bun.sleep(250)]);
		}
	}

	private send(
		socket: ServerWebSocket<WebSocketData>,
		bytes: Uint8Array,
	): void {
		if (socket.readyState !== WebSocket.OPEN) return;
		try {
			if (socket.send(bytes) > 0) return;
		} catch (error) {
			console.error(
				`Web TUI send failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		}
		this.releaseSocket(socket);
		this.clients.delete(socket);
		if (this.activeSocket === socket) this.activeSocket = null;
		socket.terminate();
	}

	private releaseSocket(socket: ServerWebSocket<WebSocketData>): void {
		const client = socket.data.client;
		socket.data.client = null;
		if (client) this.host.detach(client);
	}

	private isAuthorized(request: Request): boolean {
		if (!this.expectedBasicAuthDigest) return true;
		const credentials = decodeBasicAuthorization(
			request.headers.get("authorization"),
		);
		const actualDigest = credentialDigest(credentials ?? "");
		return timingSafeEqual(this.expectedBasicAuthDigest, actualDigest);
	}

	private authenticationRequiredResponse(): Response {
		return new Response("Authentication required", {
			status: 401,
			headers: {
				"cache-control": "no-store",
				"www-authenticate": 'Basic realm="Kit web TUI mode", charset="UTF-8"',
			},
		});
	}

	private isAllowedWebSocketRequest(request: Request, url: URL): boolean {
		return (
			this.isAllowedHost(url.host) &&
			this.isAllowedOrigin(
				request.headers.get("origin"),
				url,
				this.options.allowOriginless === true,
			)
		);
	}

	private isAllowedOrigin(
		origin: string | null,
		url: URL,
		allowOriginless: boolean,
	): boolean {
		if (!origin) return allowOriginless;
		const normalizedOrigin = normalizeOrigin(origin);
		if (!normalizedOrigin) return false;
		return (
			this.options.allowedOrigins?.includes("*") === true ||
			this.allowedOrigins(url).has(normalizedOrigin)
		);
	}

	private allowedOrigins(url: URL): Set<string> {
		const origins = new Set<string>([url.origin.toLowerCase()]);
		for (const value of this.options.allowedOrigins ?? []) {
			if (value === "*") continue;
			const origin = normalizeOrigin(value);
			if (origin) origins.add(origin);
		}
		return origins;
	}

	private isAllowedHost(host: string): boolean {
		return (
			this.options.allowedHosts?.includes("*") === true ||
			this.allowedHosts().has(host.toLowerCase())
		);
	}

	private allowedHosts(): Set<string> {
		const hostname =
			this.server?.hostname ?? this.options.hostname ?? "127.0.0.1";
		const port = this.server?.port ?? this.options.port ?? 4783;
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
