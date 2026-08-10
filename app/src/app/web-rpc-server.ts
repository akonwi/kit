import { randomUUID } from "node:crypto";
// @ts-expect-error: Bun's text loader embeds non-TypeScript browser assets.
import micaCss from "@akonwi/mica/mica.css" with { type: "text" };
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
import {
	RPC_PROTOCOL_VERSION,
	type RpcCommand,
	type RpcConnectionSnapshot,
	type RpcEventListener,
	type RpcWriter,
} from "./rpc-session-host";

export type WebRpcHost = {
	subscribe(listener: RpcEventListener): () => void;
	handleCommand(command: RpcCommand, respond: RpcWriter): Promise<void>;
	getConnectionSnapshot(maxMessages?: number): RpcConnectionSnapshot;
};

export type WebRpcServerOptions = {
	hostname?: string;
	port?: number;
	allowedHosts?: string[];
	allowedOrigins?: string[];
	allowOriginless?: boolean;
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

type MessageChunkToken = {
	bytes: Buffer;
};

const MAX_MULTIPART_BODY_BYTES = MAX_REMOTE_ATTACHMENT_BYTES + 1024 * 1024;
const MAX_CONCURRENT_UPLOADS = 4;
const MAX_SNAPSHOT_MESSAGES = 200;
const MAX_SNAPSHOT_BYTES = 64 * 1024;
const MAX_WEB_MESSAGE_PAGE_SIZE = 50;
const MAX_WEB_CHUNK_BYTES = 32 * 1024;
const MAX_MESSAGE_CHUNK_TOKENS = 1024;
const MAX_MESSAGE_CHUNK_CACHE_BYTES = 16 * 1024 * 1024;
const MAX_INLINE_INTERACTION_BYTES = 16 * 1024;

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

const WEB_ASSETS = new Map<string, { body: string; contentType: string }>([
	[
		"/assets/mica.css",
		{ body: micaCss, contentType: "text/css; charset=utf-8" },
	],
	[
		"/assets/client.css",
		{ body: clientCss, contentType: "text/css; charset=utf-8" },
	],
]);

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

function webDocumentHeaders(url: URL): HeadersInit {
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
	private readonly persistentToasts = new Map<string, unknown>();
	private readonly messageChunkTokens = new Map<string, MessageChunkToken>();
	private messageChunkCacheBytes = 0;

	constructor(
		private readonly rpcHost: WebRpcHost,
		private readonly options: WebRpcServerOptions = {},
	) {
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
						headers: webDocumentHeaders(url),
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
		if (command.type !== "get_messages") return command;
		const limit =
			typeof command.limit === "number"
				? Math.min(command.limit, MAX_WEB_MESSAGE_PAGE_SIZE)
				: MAX_WEB_MESSAGE_PAGE_SIZE;
		return {
			...command,
			offset: command.offset ?? 0,
			limit,
		};
	}

	private handleMessageChunkCommand(
		socket: ServerWebSocket<WebSocketData>,
		command: RpcCommand,
	): void {
		try {
			const token = command.token;
			if (typeof token !== "string" || !token) {
				throw new Error("token must be a non-empty string");
			}
			const metadata = this.messageChunkTokens.get(token);
			if (!metadata) throw new Error("Message chunk token is unavailable");
			const offset = command.offset ?? 0;
			const maxBytes = command.maxBytes ?? MAX_WEB_CHUNK_BYTES;
			if (
				typeof offset !== "number" ||
				!Number.isSafeInteger(offset) ||
				offset < 0
			) {
				throw new Error("offset must be a non-negative integer");
			}
			if (
				typeof maxBytes !== "number" ||
				!Number.isSafeInteger(maxBytes) ||
				maxBytes < 1 ||
				maxBytes > MAX_WEB_CHUNK_BYTES
			) {
				throw new Error(
					`maxBytes must be between 1 and ${MAX_WEB_CHUNK_BYTES}`,
				);
			}
			const { bytes } = metadata;
			if (offset > bytes.length)
				throw new Error("offset exceeds serialized data");
			const end = Math.min(bytes.length, offset + maxBytes);
			this.send(socket, {
				...(command.id === undefined ? {} : { id: command.id }),
				type: "response",
				command: command.type,
				success: true,
				data: {
					token,
					encoding: "base64-json",
					data: bytes.subarray(offset, end).toString("base64"),
					offset,
					nextOffset: end,
					totalBytes: bytes.length,
					complete: end === bytes.length,
				},
			});
		} catch (error) {
			this.send(socket, {
				...(command.id === undefined ? {} : { id: command.id }),
				type: "response",
				command: command.type,
				success: false,
				error: parseError(error),
			});
		}
	}

	private serializedProjectedMessage(message: unknown): Buffer {
		const serialized = JSON.stringify(this.projectRecord(message));
		if (serialized === undefined)
			throw new Error("Message cannot be serialized");
		return Buffer.from(serialized, "utf8");
	}

	private createMessageReference(message: unknown, messageIndex: number) {
		const bytes = this.serializedProjectedMessage(message);
		const identity = isRecord(message)
			? {
					...(typeof message.role === "string" ? { role: message.role } : {}),
					...(typeof message.messageId === "string"
						? { messageId: message.messageId }
						: {}),
					...(typeof message.turnId === "string"
						? { turnId: message.turnId }
						: {}),
				}
			: {};
		if (bytes.length > MAX_MESSAGE_CHUNK_CACHE_BYTES) {
			return {
				type: "message_unavailable",
				...identity,
				messageIndex,
				serializedBytes: bytes.length,
				reason: "exceeds_recovery_limit",
			};
		}
		while (
			this.messageChunkTokens.size >= MAX_MESSAGE_CHUNK_TOKENS ||
			this.messageChunkCacheBytes + bytes.length > MAX_MESSAGE_CHUNK_CACHE_BYTES
		) {
			const oldest = this.messageChunkTokens.keys().next().value;
			if (typeof oldest !== "string") break;
			const removed = this.messageChunkTokens.get(oldest);
			this.messageChunkTokens.delete(oldest);
			if (removed) this.messageChunkCacheBytes -= removed.bytes.length;
		}
		const token = randomUUID();
		this.messageChunkTokens.set(token, { bytes });
		this.messageChunkCacheBytes += bytes.length;
		return {
			type: "message_reference",
			...identity,
			messageIndex,
			token,
			serializedBytes: bytes.length,
			recoveryCommand: "get_message_chunk",
		};
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
		if (
			!isRecord(record) ||
			!isRecord(record.data) ||
			!Array.isArray(record.data.messages)
		) {
			return this.projectRecord(record);
		}
		const rawMessages = record.data.messages;
		const offset =
			typeof record.data.offset === "number" ? record.data.offset : 0;
		const totalMessageCount =
			typeof record.data.totalMessageCount === "number"
				? record.data.totalMessageCount
				: rawMessages.length;
		const response = this.projectRecord({
			...record,
			data: { ...record.data, messages: [] },
		}) as Record<string, unknown> & {
			data: Record<string, unknown> & { messages: unknown[] };
		};
		let consumed = 0;
		let contentOmitted = false;
		for (const [index, rawMessage] of rawMessages.entries()) {
			const message = this.projectMessage(rawMessage, offset + index);
			response.data.messages.push(message);
			if (this.serializedBytes(response) <= MAX_SNAPSHOT_BYTES) {
				consumed += 1;
				continue;
			}
			response.data.messages.pop();
			if (consumed === 0) {
				response.data.messages.push(
					this.createMessageReference(message, offset),
				);
				consumed = 1;
				contentOmitted = true;
			}
			break;
		}
		if (contentOmitted || offset + consumed < totalMessageCount) {
			response.data.messagesTruncated = true;
			response.data.hasMore = offset + consumed < totalMessageCount;
			response.data.offset = offset;
			response.data.totalMessageCount = totalMessageCount;
		}
		return response;
	}

	private projectMessage(message: unknown, messageIndex: number): unknown {
		try {
			return this.projectRecord(message);
		} catch {
			return {
				type: "message_unavailable",
				messageIndex,
				reason: "projection_failed",
			};
		}
	}

	private boundInteractionResponse(record: unknown): unknown {
		if (
			!isRecord(record) ||
			!isRecord(record.data) ||
			!Array.isArray(record.data.requests)
		) {
			return this.projectRecord(record);
		}
		const offset =
			typeof record.data.offset === "number" ? record.data.offset : 0;
		const response = this.projectRecord({
			...record,
			data: { ...record.data, requests: [] },
		}) as Record<string, unknown> & {
			data: Record<string, unknown> & { requests: unknown[] };
		};
		let consumed = 0;
		for (const rawRequest of record.data.requests) {
			const request = this.interactionReference(
				this.projectRecord(rawRequest),
				offset + consumed,
			);
			response.data.requests.push(request);
			if (this.serializedBytes(response) > MAX_SNAPSHOT_BYTES) {
				response.data.requests.pop();
				break;
			}
			consumed += 1;
		}
		if (consumed < record.data.requests.length) {
			response.data.hasMore = true;
			response.data.requestsTruncated = true;
		}
		return response;
	}

	private interactionReference(request: unknown, index: number): unknown {
		if (
			!isRecord(request) ||
			this.serializedBytes(request) <= MAX_INLINE_INTERACTION_BYTES
		) {
			return request;
		}
		return {
			id: request.id,
			kind: request.kind,
			createdAt: request.createdAt,
			requestIndex: index,
			payloadOmitted: true,
			recoveryCommand: "get_pending_interaction_chunk",
		};
	}

	private boundedConnectionSnapshot(): Record<string, unknown> {
		const snapshot = this.rpcHost.getConnectionSnapshot(MAX_SNAPSHOT_MESSAGES);
		const pendingInteractions: unknown[] = [];
		const toasts: unknown[] = [];
		const record: Record<string, unknown> = {
			state: this.projectRecord(snapshot.state),
			messages: [],
			messageOffset: snapshot.messageOffset + snapshot.messages.length,
			totalMessageCount: snapshot.totalMessageCount,
			pendingInteractions,
			pendingInteractionOffset: 0,
			totalPendingInteractionCount: snapshot.pendingInteractions.length,
			pendingInteractionGeneration: snapshot.pendingInteractionGeneration,
		};
		if (this.persistentToasts.size > 0) record.toasts = toasts;
		for (const rawToast of this.persistentToasts.values()) {
			toasts.push(this.projectRecord(rawToast));
			if (this.serializedBytes(record) > MAX_SNAPSHOT_BYTES) {
				toasts.pop();
				break;
			}
		}
		if (toasts.length === 0) delete record.toasts;
		let pendingTruncated = false;
		for (const [
			index,
			rawInteraction,
		] of snapshot.pendingInteractions.entries()) {
			const interaction = this.interactionReference(
				this.projectRecord(rawInteraction),
				index,
			);
			pendingInteractions.push(interaction);
			if (this.serializedBytes(record) > MAX_SNAPSHOT_BYTES) {
				pendingInteractions.pop();
				pendingTruncated = true;
				break;
			}
			if (isRecord(interaction) && interaction.payloadOmitted === true) {
				pendingTruncated = true;
			}
		}
		if (pendingTruncated) record.pendingInteractionsTruncated = true;

		const messages = record.messages as unknown[];
		let messageOffset = snapshot.messageOffset + snapshot.messages.length;
		for (let index = snapshot.messages.length - 1; index >= 0; index -= 1) {
			const messageIndex = snapshot.messageOffset + index;
			const message = this.projectMessage(
				snapshot.messages[index],
				messageIndex,
			);
			messages.unshift(message);
			if (this.serializedBytes(record) > MAX_SNAPSHOT_BYTES) {
				messages.shift();
				if (messages.length === 0) {
					messages.unshift(this.createMessageReference(message, messageIndex));
					messageOffset = messageIndex;
					record.messageOffset = messageOffset;
				}
				break;
			}
			messageOffset = snapshot.messageOffset + index;
			record.messageOffset = messageOffset;
		}
		if (messageOffset > 0) record.messagesTruncated = true;
		return record;
	}

	private serializedBytes(value: unknown): number {
		try {
			return Buffer.byteLength(JSON.stringify(value), "utf8");
		} catch {
			return Number.POSITIVE_INFINITY;
		}
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
		if (isRecord(record) && record.type === "agent.end") {
			const projected = { ...record };
			delete projected.messages;
			return this.projectValue(projected, new WeakSet());
		}
		return this.projectValue(record, new WeakSet());
	}

	private projectValue(value: unknown, ancestors: WeakSet<object>): unknown {
		if (typeof value === "bigint") return value.toString();
		if (Array.isArray(value)) {
			if (ancestors.has(value)) return "[Circular]";
			ancestors.add(value);
			try {
				return value.map((item) => this.projectValue(item, ancestors));
			} finally {
				ancestors.delete(value);
			}
		}
		if (!isRecord(value)) return value;
		if (ancestors.has(value)) return "[Circular]";
		ancestors.add(value);
		try {
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
					this.projectValue(item, ancestors),
				]),
			);
		} finally {
			ancestors.delete(value);
		}
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
