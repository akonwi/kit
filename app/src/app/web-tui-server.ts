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

import jetbrainsMonoItalic from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-italic.woff2" with {
	type: "file",
};
import jetbrainsMonoNormal from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-normal.woff2" with {
	type: "file",
};
import type { Server, ServerWebSocket } from "bun";
import ghosttyWasm from "ghostty-web/ghostty-vt.wasm" with { type: "file" };
import type { BrowserTheme } from "../web-tui/browser-theme";
import tuiHtml from "../web-tui/index.html" with { type: "text" };
// @ts-expect-error: Bun's text loader embeds non-TypeScript browser assets.
import tuiCss from "../web-tui/tui.css" with { type: "text" };
import {
	WebAccessPolicy,
	type WebBasicAuthCredentials,
} from "./web-access-policy";
import { clampTuiSize } from "./web-tui-bridge";

export type { WebBasicAuthCredentials as WebTuiBasicAuthCredentials } from "./web-access-policy";

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

export type WebTuiServerOptions = {
	hostname?: string;
	port?: number;
	publicUrl?: string;
	allowedHosts?: string[];
	allowedOrigins?: string[];
	allowOriginless?: boolean;
	basicAuth?: WebBasicAuthCredentials;
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

function tuiDocumentHeaders(
	url: URL,
	accessPolicy: WebAccessPolicy,
): HeadersInit {
	const webSocketOrigins = accessPolicy.webSocketConnectSources(url);
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
	private readonly accessPolicy: WebAccessPolicy;
	private browserTheme: BrowserTheme | null = null;

	constructor(
		private readonly host: WebTuiHost,
		private readonly options: WebTuiServerOptions = {},
	) {
		this.accessPolicy = new WebAccessPolicy({
			...options,
			authRealm: "Kit web TUI mode",
		});
	}

	get clientCount(): number {
		return this.activeSocket ? 1 : 0;
	}

	setBrowserTheme(theme: BrowserTheme): void {
		this.browserTheme = { ...theme };
		const socket = this.activeSocket;
		if (socket) this.sendTheme(socket);
	}

	start(): { hostname: string; port: number; url: string } {
		if (this.server) throw new Error("Web TUI server is already running");
		const server = Bun.serve<WebSocketData>({
			hostname: this.options.hostname ?? "127.0.0.1",
			port: this.options.port ?? 4783,
			fetch: async (request, bunServer) => {
				const url = new URL(request.url);
				if (!this.accessPolicy.isAllowedHost(url.host)) {
					return new Response("Host not allowed", { status: 403 });
				}
				const isWebSocketRequest = url.pathname === "/api/tui";
				if (
					isWebSocketRequest &&
					!this.accessPolicy.isAllowedWebSocketRequest(request, url)
				) {
					return new Response("Origin or host not allowed", { status: 403 });
				}
				if (!this.accessPolicy.isAuthorized(request)) {
					return this.accessPolicy.authenticationRequiredResponse();
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
						headers: tuiDocumentHeaders(url, this.accessPolicy),
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
							this.sendTheme(socket);
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
		const hostname = server.hostname ?? this.options.hostname ?? "127.0.0.1";
		const port = server.port ?? this.options.port ?? 4783;
		this.accessPolicy.setListenerAddress(hostname, port);
		return {
			hostname,
			port,
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
			let stopError: unknown;
			const stopped = server.stop(true).then(
				() => true,
				(error) => {
					stopError = error;
					return false;
				},
			);
			// Bun can leave this promise pending for a terminated WebSocket peer
			// even though force-stop has already closed the listening socket.
			const completed = await Promise.race([
				stopped,
				Bun.sleep(250).then(() => false),
			]);
			if (!completed) {
				await this.verifyListenerReleased(
					server.hostname ?? this.options.hostname ?? "127.0.0.1",
					server.port ?? this.options.port ?? 4783,
				);
			}
			if (stopError) {
				console.error(
					`Web TUI server stop failed: ${stopError instanceof Error ? stopError.message : String(stopError)}`,
				);
			}
		}
	}

	private async verifyListenerReleased(
		hostname: string,
		port: number,
	): Promise<void> {
		let probe: Server<undefined>;
		try {
			probe = Bun.serve({
				hostname,
				port,
				fetch: () => new Response("shutdown probe"),
			});
		} catch (error) {
			throw new Error(`Web TUI server did not release ${hostname}:${port}`, {
				cause: error,
			});
		}
		await probe.stop(true);
	}

	private sendTheme(socket: ServerWebSocket<WebSocketData>): void {
		if (!this.browserTheme) return;
		this.send(
			socket,
			JSON.stringify({ type: "theme", theme: this.browserTheme }),
		);
	}

	private send(
		socket: ServerWebSocket<WebSocketData>,
		data: string | Uint8Array,
	): void {
		if (socket.readyState !== WebSocket.OPEN) return;
		try {
			if (socket.send(data) > 0) return;
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
}
