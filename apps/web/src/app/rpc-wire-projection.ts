import { randomUUID } from "node:crypto";
import type { RpcCommand, RpcConnectionSnapshot } from "@akonwi/kit-protocol";

export const MAX_RPC_WIRE_SNAPSHOT_MESSAGES = 200;
export const MAX_RPC_WIRE_SNAPSHOT_BYTES = 64 * 1024;
export const MAX_RPC_WIRE_MESSAGE_PAGE_SIZE = 50;
export const MAX_RPC_WIRE_CHUNK_BYTES = 32 * 1024;
export const MAX_RPC_WIRE_MESSAGE_TOKENS = 1024;
export const MAX_RPC_WIRE_MESSAGE_CACHE_BYTES = 16 * 1024 * 1024;
export const MAX_RPC_WIRE_INLINE_INTERACTION_BYTES = 16 * 1024;

type MessageChunkToken = {
	bytes: Buffer;
};

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

export class RpcWireProjection {
	private readonly messageChunkTokens = new Map<string, MessageChunkToken>();
	private messageChunkCacheBytes = 0;

	prepareCommand(command: RpcCommand): RpcCommand {
		if (command.type !== "get_messages") return command;
		const limit =
			typeof command.limit === "number"
				? Math.min(command.limit, MAX_RPC_WIRE_MESSAGE_PAGE_SIZE)
				: MAX_RPC_WIRE_MESSAGE_PAGE_SIZE;
		return {
			...command,
			offset: command.offset ?? 0,
			limit,
		};
	}

	prepareResponse(command: RpcCommand, record: unknown): unknown {
		if (command.type === "get_capabilities") {
			return this.prepareCapabilities(record);
		}
		if (command.type === "get_messages") {
			return this.boundMessageResponse(record);
		}
		if (command.type === "get_pending_interactions") {
			return this.boundInteractionResponse(record);
		}
		return this.projectRecord(record);
	}

	messageChunkResponse(command: RpcCommand): unknown {
		try {
			const token = command.token;
			if (typeof token !== "string" || !token) {
				throw new Error("token must be a non-empty string");
			}
			const metadata = this.messageChunkTokens.get(token);
			if (!metadata) throw new Error("Message chunk token is unavailable");
			const offset = command.offset ?? 0;
			const maxBytes = command.maxBytes ?? MAX_RPC_WIRE_CHUNK_BYTES;
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
				maxBytes > MAX_RPC_WIRE_CHUNK_BYTES
			) {
				throw new Error(
					`maxBytes must be between 1 and ${MAX_RPC_WIRE_CHUNK_BYTES}`,
				);
			}
			const { bytes } = metadata;
			if (offset > bytes.length)
				throw new Error("offset exceeds serialized data");
			const end = Math.min(bytes.length, offset + maxBytes);
			return {
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
			};
		} catch (error) {
			return {
				...(command.id === undefined ? {} : { id: command.id }),
				type: "response",
				command: command.type,
				success: false,
				error: errorMessage(error),
			};
		}
	}

	private prepareCapabilities(record: unknown): unknown {
		if (
			!isRecord(record) ||
			record.success !== true ||
			!isRecord(record.data)
		) {
			return this.projectRecord(record);
		}
		const commands = Array.isArray(record.data.commands)
			? [...record.data.commands]
			: [];
		if (!commands.includes("get_message_chunk"))
			commands.push("get_message_chunk");
		const limits = isRecord(record.data.limits) ? record.data.limits : {};
		const pagination = isRecord(limits.pagination) ? limits.pagination : {};
		const recovery = isRecord(limits.recovery) ? limits.recovery : {};
		return this.projectRecord({
			...record,
			data: {
				...record.data,
				commands,
				limits: {
					...limits,
					pagination: {
						...pagination,
						messages: {
							defaultPageSize: MAX_RPC_WIRE_MESSAGE_PAGE_SIZE,
							maxPageSize: MAX_RPC_WIRE_MESSAGE_PAGE_SIZE,
						},
					},
					snapshot: {
						maxMessages: MAX_RPC_WIRE_SNAPSHOT_MESSAGES,
						maxBytes: MAX_RPC_WIRE_SNAPSHOT_BYTES,
					},
					recovery: {
						...recovery,
						message: {
							maxChunkBytes: MAX_RPC_WIRE_CHUNK_BYTES,
							maxTotalBytes: MAX_RPC_WIRE_MESSAGE_CACHE_BYTES,
							maxCachedBytes: MAX_RPC_WIRE_MESSAGE_CACHE_BYTES,
							maxTokens: MAX_RPC_WIRE_MESSAGE_TOKENS,
						},
						pendingInteraction: {
							...(isRecord(recovery.pendingInteraction)
								? recovery.pendingInteraction
								: {}),
							maxInlineBytes: MAX_RPC_WIRE_INLINE_INTERACTION_BYTES,
						},
					},
				},
			},
		});
	}

	boundedConnectionSnapshot(
		snapshot: RpcConnectionSnapshot,
		persistentToasts: Iterable<unknown> = [],
	): Record<string, unknown> {
		const pendingInteractions: unknown[] = [];
		const toasts: unknown[] = [];
		const record: Record<string, unknown> = {
			state: this.projectRecord(snapshot.state),
			messages: [],
			...(snapshot.chrome
				? { chrome: this.projectRecord(snapshot.chrome) }
				: {}),
			messageOffset: snapshot.messageOffset + snapshot.messages.length,
			totalMessageCount: snapshot.totalMessageCount,
			pendingInteractions,
			pendingInteractionOffset: 0,
			totalPendingInteractionCount: snapshot.pendingInteractions.length,
			pendingInteractionGeneration: snapshot.pendingInteractionGeneration,
		};
		for (const rawToast of persistentToasts) {
			toasts.push(this.projectRecord(rawToast));
			record.toasts = toasts;
			if (this.serializedBytes(record) > MAX_RPC_WIRE_SNAPSHOT_BYTES) {
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
			if (this.serializedBytes(record) > MAX_RPC_WIRE_SNAPSHOT_BYTES) {
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
			if (this.serializedBytes(record) > MAX_RPC_WIRE_SNAPSHOT_BYTES) {
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

	projectRecord(record: unknown): unknown {
		if (isRecord(record) && record.type === "agent.end") {
			const projected = { ...record };
			delete projected.messages;
			return this.projectValue(projected, new WeakSet());
		}
		return this.projectValue(record, new WeakSet());
	}

	serializedBytes(value: unknown): number {
		try {
			return Buffer.byteLength(JSON.stringify(value), "utf8");
		} catch {
			return Number.POSITIVE_INFINITY;
		}
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
			if (this.serializedBytes(response) <= MAX_RPC_WIRE_SNAPSHOT_BYTES) {
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
			if (this.serializedBytes(response) > MAX_RPC_WIRE_SNAPSHOT_BYTES) {
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

	private createMessageReference(message: unknown, messageIndex: number) {
		const serialized = JSON.stringify(this.projectRecord(message));
		if (serialized === undefined)
			throw new Error("Message cannot be serialized");
		const bytes = Buffer.from(serialized, "utf8");
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
		if (bytes.length > MAX_RPC_WIRE_MESSAGE_CACHE_BYTES) {
			return {
				type: "message_unavailable",
				...identity,
				messageIndex,
				serializedBytes: bytes.length,
				reason: "exceeds_recovery_limit",
			};
		}
		while (
			this.messageChunkTokens.size >= MAX_RPC_WIRE_MESSAGE_TOKENS ||
			this.messageChunkCacheBytes + bytes.length >
				MAX_RPC_WIRE_MESSAGE_CACHE_BYTES
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

	private interactionReference(request: unknown, index: number): unknown {
		if (
			!isRecord(request) ||
			this.serializedBytes(request) <= MAX_RPC_WIRE_INLINE_INTERACTION_BYTES
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
}
