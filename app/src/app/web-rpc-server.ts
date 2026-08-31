import {
	isRpcCommand,
	RPC_PROTOCOL_VERSION,
	type RpcCommand,
	type RpcConnectionSnapshot,
} from "@akonwi/kit-protocol";
// @ts-expect-error: Bun's text loader embeds non-TypeScript browser assets.
import micaCss from "@akonwi/mica/mica.css" with { type: "text" };
import jetbrainsMonoItalic from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-italic.woff2" with {
	type: "file",
};
import jetbrainsMonoNormal from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-normal.woff2" with {
	type: "file",
};
import type { Server, ServerWebSocket } from "bun";
// @ts-expect-error: Bun's text loader embeds non-TypeScript browser assets.
import clientCss from "../web/client.css" with { type: "text" };
import clientHtml from "../web/index.html" with { type: "text" };
import {
	MAX_REMOTE_ATTACHMENT_BYTES,
	RemoteAttachmentError,
	type RemoteAttachmentStore,
} from "./remote-attachment-store";
import {
	RemoteEventJournal,
	type SequencedRemoteEvent,
} from "./remote-event-journal";
import type { RpcEventListener, RpcWriter } from "./rpc-session-host";
import {
	MAX_RPC_WIRE_INLINE_INTERACTION_BYTES as MAX_INLINE_INTERACTION_BYTES,
	MAX_RPC_WIRE_MESSAGE_CACHE_BYTES as MAX_MESSAGE_CHUNK_CACHE_BYTES,
	MAX_RPC_WIRE_MESSAGE_TOKENS as MAX_MESSAGE_CHUNK_TOKENS,
	MAX_RPC_WIRE_SNAPSHOT_BYTES as MAX_SNAPSHOT_BYTES,
	MAX_RPC_WIRE_SNAPSHOT_MESSAGES as MAX_SNAPSHOT_MESSAGES,
	MAX_RPC_WIRE_CHUNK_BYTES as MAX_WEB_CHUNK_BYTES,
	MAX_RPC_WIRE_MESSAGE_PAGE_SIZE as MAX_WEB_MESSAGE_PAGE_SIZE,
	RpcWireProjection,
} from "./rpc-wire-projection";
import {
	WebAccessPolicy,
	type WebBasicAuthCredentials,
} from "./web-access-policy";

export type WebRpcHost = {
	subscribe(listener: RpcEventListener): () => void;
	handleCommand(command: RpcCommand, respond: RpcWriter): Promise<void>;
	getConnectionSnapshot(maxMessages?: number): RpcConnectionSnapshot;
};

export type { WebBasicAuthCredentials } from "./web-access-policy";

export type WebRpcServerOptions = {
	hostname?: string;
	port?: number;
	publicUrl?: string;
	allowedHosts?: string[];
	allowedOrigins?: string[];
	allowOriginless?: boolean;
	basicAuth?: WebBasicAuthCredentials;
	attachments?: RemoteAttachmentStore;
	eventStreamId?: string;
	eventHistoryMaxEvents?: number;
	eventHistoryMaxBytes?: number;
};

type ResumeCursor = {
	requested: boolean;
	valid: boolean;
	streamId?: string;
	after?: number;
};

type WebSocketData = {
	resume: ResumeCursor;
};

const MAX_MULTIPART_BODY_BYTES = MAX_REMOTE_ATTACHMENT_BYTES + 1024 * 1024;
const MAX_CONCURRENT_UPLOADS = 4;

declare const __KIT_WEB_CLIENT_JS__: string | undefined;

let developmentWebClient: Promise<string> | null = null;

function webClientJavaScript(): Promise<string> {
	if (typeof __KIT_WEB_CLIENT_JS__ === "string") {
		return Promise.resolve(__KIT_WEB_CLIENT_JS__);
	}
	const developmentBuilderUrl = new URL(
		"../web/build-client.ts",
		import.meta.url,
	).href;
	developmentWebClient ??= import(developmentBuilderUrl).then(
		({ buildWebClient }: typeof import("../web/build-client")) =>
			buildWebClient(),
	);
	return developmentWebClient;
}

const WEB_ASSETS = new Map<
	string,
	{ body: string | Blob; contentType: string }
>([
	[
		"/assets/mica.css",
		{ body: micaCss, contentType: "text/css; charset=utf-8" },
	],
	[
		"/assets/client.css",
		{ body: clientCss, contentType: "text/css; charset=utf-8" },
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

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseCommand(message: string | Buffer): RpcCommand {
	const parsed: unknown = JSON.parse(
		typeof message === "string" ? message : message.toString("utf8"),
	);
	if (!isRpcCommand(parsed)) {
		throw new Error("Command must be an object with a string type");
	}
	return parsed;
}

function parseError(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

function webDocumentHeaders(
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
			"script-src 'self'",
			"style-src 'self'",
		].join("; "),
		"referrer-policy": "no-referrer",
		"x-content-type-options": "nosniff",
	};
}

function parseResumeCursor(url: URL): ResumeCursor {
	const streamId = url.searchParams.get("streamId");
	const afterValue = url.searchParams.get("after");
	if (streamId === null && afterValue === null) {
		return { requested: false, valid: true };
	}
	if (
		!streamId ||
		streamId.length > 128 ||
		afterValue === null ||
		!/^\d+$/.test(afterValue)
	) {
		return { requested: true, valid: false };
	}
	const after = Number(afterValue);
	if (!Number.isSafeInteger(after)) {
		return { requested: true, valid: false };
	}
	return { requested: true, valid: true, streamId, after };
}

export class WebRpcServer {
	private server: Server<WebSocketData> | null = null;
	private unsubscribeHost: (() => void) | null = null;
	private activeUploads = 0;
	private readonly clients = new Set<ServerWebSocket<WebSocketData>>();
	private readonly journal: RemoteEventJournal;
	private readonly projection = new RpcWireProjection();
	private readonly persistentToasts = new Map<string, unknown>();
	private readonly accessPolicy: WebAccessPolicy;

	constructor(
		private readonly rpcHost: WebRpcHost,
		private readonly options: WebRpcServerOptions = {},
	) {
		this.accessPolicy = new WebAccessPolicy({
			...options,
			authRealm: "Kit web mode",
		});
		this.journal = new RemoteEventJournal({
			streamId: options.eventStreamId,
			maxEvents: options.eventHistoryMaxEvents,
			maxBytes: options.eventHistoryMaxBytes,
		});
	}

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
				if (!this.accessPolicy.isAllowedHost(url.host)) {
					return new Response("Host not allowed", { status: 403 });
				}
				const isAttachmentRequest =
					url.pathname === "/api/attachments" ||
					url.pathname.startsWith("/api/attachments/");
				const isWebSocketRequest = url.pathname === "/api/rpc";
				if (
					(isAttachmentRequest &&
						!this.accessPolicy.isAllowedHttpRequest(request, url)) ||
					(isWebSocketRequest &&
						!this.accessPolicy.isAllowedWebSocketRequest(request, url))
				) {
					return new Response("Origin or host not allowed", { status: 403 });
				}
				if (isAttachmentRequest && request.method === "OPTIONS") {
					return this.handleAttachmentRequest(request, url);
				}
				if (!this.accessPolicy.isAuthorized(request)) {
					return this.accessPolicy.authenticationRequiredResponse();
				}
				if (url.pathname === "/assets/client.js") {
					return new Response(await webClientJavaScript(), {
						headers: {
							"cache-control": "no-cache",
							"content-type": "text/javascript; charset=utf-8",
							"x-content-type-options": "nosniff",
						},
					});
				}
				const webAsset = WEB_ASSETS.get(url.pathname);
				if (webAsset) {
					return new Response(webAsset.body, {
						headers: {
							"cache-control": "no-cache",
							"content-type": webAsset.contentType,
							"x-content-type-options": "nosniff",
						},
					});
				}
				if (url.pathname === "/api/health") {
					return Response.json({
						ok: true,
						mode: "web",
						clients: this.clients.size,
					});
				}
				if (isAttachmentRequest) {
					return this.handleAttachmentRequest(request, url);
				}
				if (isWebSocketRequest) {
					if (
						bunServer.upgrade(request, {
							data: { resume: parseResumeCursor(url) },
						})
					) {
						return undefined;
					}
					return new Response("WebSocket upgrade required", { status: 426 });
				}
				if (url.pathname === "/") {
					return new Response(clientHtml as unknown as string, {
						headers: webDocumentHeaders(url, this.accessPolicy),
					});
				}
				return new Response("Not found", { status: 404 });
			},
			websocket: {
				maxPayloadLength: 1024 * 1024,
				backpressureLimit: 16 * 1024 * 1024,
				closeOnBackpressureLimit: true,
				open: (socket) => {
					if (this.synchronizeClient(socket)) this.clients.add(socket);
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
					if (command.type === "get_message_chunk") {
						this.handleMessageChunkCommand(socket, command);
						return;
					}
					const prepared = this.prepareWebCommand(command);
					void this.rpcHost.handleCommand(prepared, async (record) => {
						this.send(socket, this.prepareWebResponse(prepared, record));
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
		this.accessPolicy.setListenerAddress(hostname, port);
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

	private send(
		socket: ServerWebSocket<WebSocketData>,
		record: unknown,
	): boolean {
		if (socket.readyState !== WebSocket.OPEN) {
			this.removeClient(socket);
			return false;
		}
		try {
			const status = socket.send(JSON.stringify(this.projectRecord(record)));
			if (status <= 0) {
				this.removeClient(socket, true);
				return false;
			}
			return true;
		} catch (error) {
			this.removeClient(socket, true);
			console.error(
				`WebSocket send failed: ${error instanceof Error ? error.message : String(error)}`,
			);
			return false;
		}
	}

	private removeClient(
		socket: ServerWebSocket<WebSocketData>,
		terminate = false,
	): void {
		this.clients.delete(socket);
		if (terminate) socket.terminate();
	}

	private broadcast(record: unknown): void {
		let event: SequencedRemoteEvent;
		try {
			const projected = this.projectRecord(record);
			this.rememberPersistentToast(projected);
			event = this.journal.append(projected);
		} catch {
			event = this.journal.append({
				type: "resync_required",
				reason: "event_projection_failed",
			});
		}
		for (const client of [...this.clients]) this.send(client, event);
	}

	private rememberPersistentToast(record: unknown): void {
		if (
			!isRecord(record) ||
			record.type !== "ui.toast.requested" ||
			!isRecord(record.toast) ||
			record.toast.persistent !== true
		) {
			return;
		}
		const key = JSON.stringify(record.toast);
		this.persistentToasts.set(key, record.toast);
		if (this.persistentToasts.size > 32) {
			const first = this.persistentToasts.keys().next().value;
			if (typeof first === "string") this.persistentToasts.delete(first);
		}
	}

	private prepareWebCommand(command: RpcCommand): RpcCommand {
		return this.projection.prepareCommand(command);
	}

	private handleMessageChunkCommand(
		socket: ServerWebSocket<WebSocketData>,
		command: RpcCommand,
	): void {
		this.send(socket, this.projection.messageChunkResponse(command));
	}

	private prepareWebResponse(command: RpcCommand, record: unknown): unknown {
		let prepared = record;
		if (
			command.type === "get_capabilities" &&
			isRecord(record) &&
			record.success === true &&
			isRecord(record.data)
		) {
			prepared = {
				...record,
				data: {
					...record.data,
					...(Array.isArray(record.data.commands)
						? {
								commands: [
									...record.data.commands,
									...(!record.data.commands.includes("get_message_chunk")
										? ["get_message_chunk"]
										: []),
								],
							}
						: {}),
					eventSequencing: {
						supported: true,
						resume: "websocket_query",
						streamId: this.journal.streamId,
						latestSequence: this.journal.latestSequence,
						...this.journal.retention,
					},
					limits: {
						...(isRecord(record.data.limits) ? record.data.limits : {}),
						attachments: {
							...(isRecord(record.data.limits) &&
							isRecord(record.data.limits.attachments)
								? record.data.limits.attachments
								: {}),
							maxConcurrentUploads: MAX_CONCURRENT_UPLOADS,
							maxRequestBytes: MAX_MULTIPART_BODY_BYTES,
						},
						pagination: {
							...(isRecord(record.data.limits) &&
							isRecord(record.data.limits.pagination)
								? record.data.limits.pagination
								: {}),
							messages: {
								defaultPageSize: MAX_WEB_MESSAGE_PAGE_SIZE,
								maxPageSize: MAX_WEB_MESSAGE_PAGE_SIZE,
							},
						},
						snapshot: {
							maxMessages: MAX_SNAPSHOT_MESSAGES,
							maxBytes: MAX_SNAPSHOT_BYTES,
						},
						recovery: {
							...(isRecord(record.data.limits) &&
							isRecord(record.data.limits.recovery)
								? record.data.limits.recovery
								: {}),
							message: {
								maxChunkBytes: MAX_WEB_CHUNK_BYTES,
								maxTotalBytes: MAX_MESSAGE_CHUNK_CACHE_BYTES,
								maxCachedBytes: MAX_MESSAGE_CHUNK_CACHE_BYTES,
								maxTokens: MAX_MESSAGE_CHUNK_TOKENS,
							},
							pendingInteraction: {
								...(isRecord(record.data.limits) &&
								isRecord(record.data.limits.recovery) &&
								isRecord(record.data.limits.recovery.pendingInteraction)
									? record.data.limits.recovery.pendingInteraction
									: {}),
								maxInlineBytes: MAX_INLINE_INTERACTION_BYTES,
							},
						},
					},
				},
			};
		}
		if (command.type === "get_messages") {
			return this.boundMessageResponse(prepared);
		}
		if (command.type === "get_pending_interactions") {
			return this.boundInteractionResponse(prepared);
		}
		return this.projectRecord(prepared);
	}

	private boundMessageResponse(record: unknown): unknown {
		return this.projection.prepareResponse({ type: "get_messages" }, record);
	}

	private boundInteractionResponse(record: unknown): unknown {
		return this.projection.prepareResponse(
			{ type: "get_pending_interactions" },
			record,
		);
	}

	private boundedConnectionSnapshot(): Record<string, unknown> {
		return this.projection.boundedConnectionSnapshot(
			this.rpcHost.getConnectionSnapshot(MAX_SNAPSHOT_MESSAGES),
			this.persistentToasts.values(),
		);
	}

	private serializedBytes(value: unknown): number {
		return this.projection.serializedBytes(value);
	}

	private synchronizeClient(socket: ServerWebSocket<WebSocketData>): boolean {
		const cursor = socket.data.resume;
		const latestSequence = this.journal.latestSequence;
		if (
			cursor.requested &&
			cursor.valid &&
			cursor.streamId === this.journal.streamId &&
			cursor.after !== undefined &&
			cursor.after <= latestSequence
		) {
			const replay = this.journal.replayAfter(cursor.after);
			if (replay) {
				if (
					!this.send(socket, {
						type: "sync",
						mode: "replay",
						protocolVersion: RPC_PROTOCOL_VERSION,
						streamId: this.journal.streamId,
						sequence: cursor.after,
						targetSequence: latestSequence,
					})
				) {
					return false;
				}
				for (const event of replay) {
					if (!this.send(socket, event)) return false;
				}
				return this.send(socket, {
					type: "sync_complete",
					mode: "replay",
					streamId: this.journal.streamId,
					sequence: latestSequence,
				});
			}
		}

		let reason = "initial";
		if (cursor.requested && !cursor.valid) reason = "invalid_cursor";
		else if (cursor.requested && cursor.streamId !== this.journal.streamId) {
			reason = "stream_changed";
		} else if (
			cursor.requested &&
			cursor.after !== undefined &&
			cursor.after > latestSequence
		) {
			reason = "invalid_cursor";
		} else if (cursor.requested) reason = "history_unavailable";

		const snapshot = this.boundedConnectionSnapshot();
		if (
			!this.send(socket, {
				type: "sync",
				mode: "snapshot",
				reason,
				protocolVersion: RPC_PROTOCOL_VERSION,
				streamId: this.journal.streamId,
				sequence: latestSequence,
				...snapshot,
			})
		) {
			return false;
		}
		return this.send(socket, {
			type: "sync_complete",
			mode: "snapshot",
			streamId: this.journal.streamId,
			sequence: latestSequence,
		});
	}

	private async handleAttachmentRequest(
		request: Request,
		url: URL,
	): Promise<Response> {
		if (!this.accessPolicy.isAllowedHttpRequest(request, url)) {
			return new Response("Origin or host not allowed", { status: 403 });
		}
		const corsHeaders = this.accessPolicy.corsHeaders(request);
		if (request.method === "OPTIONS") {
			return new Response(null, {
				status: 204,
				headers: {
					...corsHeaders,
					"access-control-allow-headers": "authorization, content-type",
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

	private projectRecord(record: unknown): unknown {
		return this.projection.projectRecord(record);
	}
}
