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
import {
	MAX_BROWSER_CLIPBOARD_BYTES,
	WEB_TUI_PROTOCOL_VERSION,
} from "../web-tui/browser-actions";
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
	initTimer: ReturnType<typeof setTimeout> | null;
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

const MAX_WEB_TUI_CONNECTIONS = 8;
const WEB_TUI_INIT_TIMEOUT_MS = 5_000;

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

type TerminalControlMessage = {
	type: "init" | "resize";
	cols: number;
	rows: number;
	protocolVersion?: number;
};
type ClipboardResultMessage = {
	type: "clipboard-result";
	id: number;
	ok: boolean;
	error?: string;
};
type ControlMessage = TerminalControlMessage | ClipboardResultMessage;

function parseControlMessage(message: string): ControlMessage | null {
	if (message.length > 512) return null;
	let parsed: unknown;
	try {
		parsed = JSON.parse(message);
	} catch {
		return null;
	}
	if (typeof parsed !== "object" || parsed === null) return null;
	const record = parsed as Record<string, unknown>;
	if (record.type === "clipboard-result") {
		if (
			!Number.isSafeInteger(record.id) ||
			(record.id as number) <= 0 ||
			typeof record.ok !== "boolean" ||
			(record.error !== undefined &&
				(typeof record.error !== "string" || record.error.length > 256))
		) {
			return null;
		}
		return record as ClipboardResultMessage;
	}
	if (record.type !== "init" && record.type !== "resize") return null;
	if (
		typeof record.cols !== "number" ||
		typeof record.rows !== "number" ||
		!Number.isFinite(record.cols) ||
		!Number.isFinite(record.rows)
	) {
		return null;
	}
	if (
		record.protocolVersion !== undefined &&
		!Number.isSafeInteger(record.protocolVersion)
	) {
		return null;
	}
	const size = clampTuiSize(record.cols, record.rows);
	return {
		type: record.type,
		cols: size.cols,
		rows: size.rows,
		...(typeof record.protocolVersion === "number"
			? { protocolVersion: record.protocolVersion }
			: {}),
	};
}

export class WebTuiServer {
	private server: Server<WebSocketData> | null = null;
	private activeSocket: ServerWebSocket<WebSocketData> | null = null;
	private readonly clients = new Set<ServerWebSocket<WebSocketData>>();
	private readonly accessPolicy: WebAccessPolicy;
	private browserTheme: BrowserTheme | null = null;
	private nextClipboardId = 1;
	private readonly pendingClipboard = new Map<
		number,
		{
			socket: ServerWebSocket<WebSocketData>;
			resolve: () => void;
			reject: (error: Error) => void;
			timer: ReturnType<typeof setTimeout>;
		}
	>();

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

	notify(message: string, title = "Kit"): boolean {
		const socket = this.activeSocket;
		if (!socket?.data.client) return false;
		const safeTitle = title.trim().slice(0, 100) || "Kit";
		const safeMessage = message.trim().slice(0, 500);
		if (!safeMessage) return false;
		return this.send(
			socket,
			JSON.stringify({
				type: "notification",
				title: safeTitle,
				message: safeMessage,
			}),
		);
	}

	bell(isError: boolean): void {
		const socket = this.activeSocket;
		if (!socket?.data.client) return;
		this.send(
			socket,
			JSON.stringify({
				type: "bell",
				kind: isError ? "error" : "attention",
			}),
		);
	}

	copyText(text: string): Promise<void> {
		const byteLength = new TextEncoder().encode(text).byteLength;
		if (byteLength > MAX_BROWSER_CLIPBOARD_BYTES) {
			return Promise.reject(
				new Error("Clipboard content exceeds the 1 MiB browser limit"),
			);
		}
		const socket = this.activeSocket;
		if (!socket?.data.client || socket.readyState !== WebSocket.OPEN) {
			return Promise.reject(new Error("No browser is connected"));
		}
		const id = this.nextClipboardId++;
		return new Promise<void>((resolve, reject) => {
			const timer = setTimeout(() => {
				this.pendingClipboard.delete(id);
				reject(new Error("Browser clipboard request timed out"));
			}, 5_000);
			this.pendingClipboard.set(id, { socket, resolve, reject, timer });
			this.send(socket, JSON.stringify({ type: "clipboard-write", id, text }));
		});
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
					if (
						bunServer.upgrade(request, {
							data: { client: null, initTimer: null },
						})
					) {
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
					// A socket is only promoted after a version-compatible init so stale
					// reconnect loops cannot evict the valid active browser.
					if (this.clients.size >= MAX_WEB_TUI_CONNECTIONS) {
						socket.close(1013, "too many browser connections");
						return;
					}
					this.clients.add(socket);
					socket.data.initTimer = setTimeout(() => {
						socket.data.initTimer = null;
						if (!socket.data.client) socket.close(4003, "init timed out");
					}, WEB_TUI_INIT_TIMEOUT_MS);
				},
				message: (socket, message) => {
					if (typeof message === "string") {
						const control = parseControlMessage(message);
						if (!control) return;
						if (control.type === "init") {
							if (control.protocolVersion !== WEB_TUI_PROTOCOL_VERSION) {
								// Pre-version clients understand 4001 as a terminal close and stop
								// reconnecting. Version-aware clients reload on 4002.
								socket.close(
									control.protocolVersion === undefined ? 4001 : 4002,
									"browser client update required",
								);
								return;
							}
							if (socket.data.initTimer) {
								clearTimeout(socket.data.initTimer);
								socket.data.initTimer = null;
							}
							if (socket.data.client) {
								if (this.activeSocket === socket) {
									this.host.resize(control.cols, control.rows);
								}
								return;
							}
							const previous = this.activeSocket;
							this.activeSocket = socket;
							if (previous && previous !== socket) {
								this.releaseSocket(previous);
								previous.close(4001, "replaced by a newer client");
							}
							const client: WebTuiClient = {
								send: (bytes) => this.send(socket, bytes),
							};
							socket.data.client = client;
							this.sendTheme(socket);
							this.host.attach(client, control.cols, control.rows);
							return;
						}
						if (this.activeSocket !== socket) return;
						if (control.type === "clipboard-result") {
							this.resolveClipboard(control);
							return;
						}
						if (socket.data.client) {
							this.host.resize(control.cols, control.rows);
						}
						return;
					}
					if (this.activeSocket !== socket) return;
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

	private resolveClipboard(result: ClipboardResultMessage): void {
		const pending = this.pendingClipboard.get(result.id);
		if (!pending || pending.socket !== this.activeSocket) return;
		this.pendingClipboard.delete(result.id);
		clearTimeout(pending.timer);
		if (result.ok) pending.resolve();
		else
			pending.reject(
				new Error(result.error || "Browser clipboard write failed"),
			);
	}

	private rejectClipboardForSocket(
		socket: ServerWebSocket<WebSocketData>,
		reason: string,
	): void {
		for (const [id, pending] of this.pendingClipboard) {
			if (pending.socket !== socket) continue;
			this.pendingClipboard.delete(id);
			clearTimeout(pending.timer);
			pending.reject(new Error(reason));
		}
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
	): boolean {
		if (socket.readyState !== WebSocket.OPEN) return false;
		try {
			// Bun returns -1 when the frame was accepted under backpressure and 0
			// only when it was dropped. The configured limit owns hard failure.
			if (socket.send(data) !== 0) return true;
		} catch (error) {
			console.error(
				`Web TUI send failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		}
		this.releaseSocket(socket);
		this.clients.delete(socket);
		if (this.activeSocket === socket) this.activeSocket = null;
		socket.terminate();
		return false;
	}

	private releaseSocket(socket: ServerWebSocket<WebSocketData>): void {
		if (socket.data.initTimer) {
			clearTimeout(socket.data.initTimer);
			socket.data.initTimer = null;
		}
		this.rejectClipboardForSocket(
			socket,
			"Browser disconnected before copying",
		);
		const client = socket.data.client;
		socket.data.client = null;
		if (client) this.host.detach(client);
	}
}
