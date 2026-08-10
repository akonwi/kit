import { isRecord } from "./client-state";
import type { RpcCommandClient } from "./rpc-transport";

export type ClientLimits = {
	maxAttachmentsPerPrompt: number;
	maxAttachmentBytes: number;
	maxTextAttachmentBytes: number;
	maxPromptAttachmentBytes: number;
	maxPromptTextBytes: number;
	messagePageSize: number;
	interactionPageSize: number;
	messageChunkBytes: number;
	messageRecoveryBytes: number;
	interactionChunkBytes: number;
	interactionRecoveryBytes: number;
};

export type RecoveredMessage = {
	message: unknown;
	rebased: boolean;
};

export type MessageRange = {
	messages: unknown[];
	totalMessageCount: number;
};

export type PendingInteractionSet = {
	requests: unknown[];
	totalRequestCount: number;
};

export type RemoteCommand = {
	id: string;
	name: string;
	description?: string;
	argName?: string;
	category?: string;
};

export type RemoteCommandList = {
	commands: RemoteCommand[];
	registryGeneration: number;
};

export type RemoteModel = {
	id: string;
	provider: string;
	name?: string;
};

const MAX_REMOTE_COMMANDS = 512;
const MAX_COMMAND_ID_LENGTH = 256;
const MAX_COMMAND_NAME_LENGTH = 256;
const MAX_COMMAND_DESCRIPTION_LENGTH = 2_048;
const MAX_COMMAND_METADATA_LENGTH = 128;
const MAX_REMOTE_MODELS = 1_024;
const MAX_MODEL_FIELD_LENGTH = 512;
const MAX_THINKING_LEVELS = 32;
const MAX_THINKING_LEVEL_LENGTH = 64;

export const DEFAULT_CLIENT_LIMITS: ClientLimits = {
	maxAttachmentsPerPrompt: 8,
	maxAttachmentBytes: 10 * 1024 * 1024,
	maxTextAttachmentBytes: 1024 * 1024,
	maxPromptAttachmentBytes: 20 * 1024 * 1024,
	maxPromptTextBytes: 1024 * 1024,
	messagePageSize: 50,
	interactionPageSize: 20,
	messageChunkBytes: 32 * 1024,
	messageRecoveryBytes: 16 * 1024 * 1024,
	interactionChunkBytes: 48 * 1024,
	interactionRecoveryBytes: 2 * 1024 * 1024,
};

function positiveInteger(value: unknown, fallback: number): number {
	return typeof value === "number" && Number.isSafeInteger(value) && value > 0
		? value
		: fallback;
}

function nonnegativeInteger(value: unknown, fallback: number): number {
	return typeof value === "number" && Number.isSafeInteger(value) && value >= 0
		? value
		: fallback;
}

function hasAsciiControlCharacter(value: string): boolean {
	for (let index = 0; index < value.length; index += 1) {
		const code = value.charCodeAt(index);
		if (code <= 31 || code === 127) return true;
	}
	return false;
}

function boundedNonemptyString(
	value: unknown,
	maxLength: number,
): string | null {
	return typeof value === "string" &&
		value.trim() === value &&
		value.length > 0 &&
		value.length <= maxLength &&
		!hasAsciiControlCharacter(value)
		? value
		: null;
}

function isRemoteImage(file: File): boolean {
	const mimeType = file.type.split(";", 1)[0]?.trim().toLowerCase();
	if (mimeType) {
		return ["image/gif", "image/jpeg", "image/png", "image/webp"].includes(
			mimeType,
		);
	}
	return /\.(?:gif|jpe?g|png|webp)$/i.test(file.name);
}

function decodeChunk(
	data: Record<string, unknown>,
	offset: number,
	maxTotalBytes: number,
	label: string,
): {
	bytes: Uint8Array;
	nextOffset: number;
	totalBytes: number;
	complete: boolean;
} {
	const totalBytes = data.totalBytes;
	if (
		typeof data.data !== "string" ||
		typeof totalBytes !== "number" ||
		!Number.isSafeInteger(totalBytes) ||
		totalBytes < 0 ||
		totalBytes > maxTotalBytes ||
		data.offset !== offset
	) {
		throw new Error(`${label} exceeds the client recovery limit`);
	}
	const binary = atob(data.data);
	const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
	const nextOffset = data.nextOffset;
	if (
		typeof nextOffset !== "number" ||
		!Number.isSafeInteger(nextOffset) ||
		nextOffset !== offset + bytes.length ||
		nextOffset > totalBytes ||
		(nextOffset === offset && data.complete !== true)
	) {
		throw new Error(`${label} chunks are not contiguous`);
	}
	const complete = data.complete === true;
	if (complete && nextOffset !== totalBytes) {
		throw new Error(`${label} payload ended at the wrong offset`);
	}
	return { bytes, nextOffset, totalBytes, complete };
}

function parseChunks(chunks: Uint8Array[]): unknown {
	const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
	const joined = new Uint8Array(total);
	let cursor = 0;
	for (const chunk of chunks) {
		joined.set(chunk, cursor);
		cursor += chunk.length;
	}
	return JSON.parse(new TextDecoder().decode(joined));
}

export class WebRemoteServices {
	private limitsValue = DEFAULT_CLIENT_LIMITS;

	constructor(
		private readonly rpc: RpcCommandClient,
		private readonly fetcher: typeof fetch = fetch,
	) {}

	installLimits(limits: ClientLimits): void {
		this.limitsValue = limits;
	}

	resetLimits(): void {
		this.limitsValue = DEFAULT_CLIENT_LIMITS;
	}

	async listModels(): Promise<RemoteModel[]> {
		const response = await this.rpc.command({ type: "get_available_models" });
		if (!isRecord(response.data) || !Array.isArray(response.data.models)) {
			throw new Error("Model list omitted models");
		}
		if (response.data.models.length > MAX_REMOTE_MODELS) {
			throw new Error("Model list exceeds the client limit");
		}
		const identities = new Set<string>();
		return response.data.models.map((value) => {
			if (!isRecord(value))
				throw new Error("Model list contains an invalid model");
			const id = boundedNonemptyString(value.id, MAX_MODEL_FIELD_LENGTH);
			const provider = boundedNonemptyString(
				value.provider,
				MAX_MODEL_FIELD_LENGTH,
			);
			const name =
				value.name === undefined
					? undefined
					: boundedNonemptyString(value.name, MAX_MODEL_FIELD_LENGTH);
			if (!id || !provider || (value.name !== undefined && !name)) {
				throw new Error("Model list contains an invalid model");
			}
			const identity = `${provider}\u0000${id}`;
			if (identities.has(identity)) {
				throw new Error("Model list contains a duplicate model");
			}
			identities.add(identity);
			return { id, provider, ...(name ? { name } : {}) };
		});
	}

	setModel(model: RemoteModel): Promise<Record<string, unknown>> {
		return this.rpc.command({
			type: "set_model",
			provider: model.provider,
			modelId: model.id,
		});
	}

	async listThinkingLevels(): Promise<string[]> {
		const response = await this.rpc.command({
			type: "get_available_thinking_levels",
		});
		if (!isRecord(response.data) || !Array.isArray(response.data.levels)) {
			throw new Error("Thinking-level list omitted levels");
		}
		if (response.data.levels.length > MAX_THINKING_LEVELS) {
			throw new Error("Thinking-level list exceeds the client limit");
		}
		const levels = response.data.levels.map((value) => {
			const level = boundedNonemptyString(value, MAX_THINKING_LEVEL_LENGTH);
			if (!level)
				throw new Error("Thinking-level list contains an invalid level");
			return level;
		});
		if (new Set(levels).size !== levels.length) {
			throw new Error("Thinking-level list contains duplicate levels");
		}
		return levels;
	}

	setThinkingLevel(level: string): Promise<Record<string, unknown>> {
		return this.rpc.command({ type: "set_thinking_level", level });
	}

	async listCommands(): Promise<RemoteCommandList> {
		const response = await this.rpc.command({ type: "list_commands" });
		if (!isRecord(response.data) || !Array.isArray(response.data.commands)) {
			throw new Error("Command list omitted commands");
		}
		const registryGeneration = response.data.registryGeneration;
		if (
			typeof registryGeneration !== "number" ||
			!Number.isSafeInteger(registryGeneration) ||
			registryGeneration < 0
		) {
			throw new Error("Command list omitted its generation");
		}
		if (response.data.commands.length > MAX_REMOTE_COMMANDS) {
			throw new Error("Command list exceeds the client limit");
		}
		const commandIds = new Set<string>();
		const commands = response.data.commands.map((value) => {
			if (!isRecord(value)) {
				throw new Error("Command list contains an invalid command");
			}
			const id = value.id;
			const name = value.name;
			if (
				typeof id !== "string" ||
				id.trim() !== id ||
				id.length === 0 ||
				id.length > MAX_COMMAND_ID_LENGTH ||
				typeof name !== "string" ||
				name.trim().length === 0 ||
				name.length > MAX_COMMAND_NAME_LENGTH ||
				(value.description !== undefined &&
					typeof value.description !== "string") ||
				(typeof value.description === "string" &&
					value.description.length > MAX_COMMAND_DESCRIPTION_LENGTH) ||
				(value.argName !== undefined && typeof value.argName !== "string") ||
				(typeof value.argName === "string" &&
					value.argName.length > MAX_COMMAND_METADATA_LENGTH) ||
				(value.category !== undefined && typeof value.category !== "string") ||
				(typeof value.category === "string" &&
					value.category.length > MAX_COMMAND_METADATA_LENGTH) ||
				commandIds.has(id)
			) {
				throw new Error("Command list contains an invalid command");
			}
			commandIds.add(id);
			return {
				id,
				name,
				...(typeof value.description === "string"
					? { description: value.description }
					: {}),
				...(typeof value.argName === "string"
					? { argName: value.argName }
					: {}),
				...(typeof value.category === "string"
					? { category: value.category }
					: {}),
			};
		});
		return { commands, registryGeneration };
	}

	activateChromeContribution(
		area: "header" | "footer",
		contributionId: string,
	): Promise<Record<string, unknown>> {
		return this.rpc.command({
			type: "activate_chrome_contribution",
			area,
			contributionId,
		});
	}

	executeCommand(
		commandId: string,
		args: string,
		registryGeneration: number,
	): Promise<Record<string, unknown>> {
		return this.rpc.command({
			type: "execute_command",
			commandId,
			registryGeneration,
			...(args.trim() ? { args } : {}),
		});
	}

	async fetchLimits(): Promise<ClientLimits> {
		const response = await this.rpc.command({ type: "get_capabilities" });
		if (!isRecord(response.data)) throw new Error("Capabilities omitted data");
		const limits = response.data.limits;
		if (!isRecord(limits)) throw new Error("Capabilities omitted limits");
		const attachments = limits.attachments;
		const pagination = limits.pagination;
		const recovery = limits.recovery;
		if (
			!isRecord(attachments) ||
			!isRecord(pagination) ||
			!isRecord(recovery)
		) {
			throw new Error("Capabilities contain invalid limits");
		}
		const messages = pagination.messages;
		const interactions = pagination.pendingInteractions;
		const messageRecovery = recovery.message;
		const interactionRecovery = recovery.pendingInteraction;
		if (
			!isRecord(messages) ||
			!isRecord(interactions) ||
			!isRecord(messageRecovery) ||
			!isRecord(interactionRecovery)
		) {
			throw new Error("Capabilities contain invalid page or recovery limits");
		}
		return {
			maxAttachmentsPerPrompt: nonnegativeInteger(
				attachments.maxFilesPerPrompt,
				this.limitsValue.maxAttachmentsPerPrompt,
			),
			maxAttachmentBytes: nonnegativeInteger(
				attachments.maxFileBytes,
				this.limitsValue.maxAttachmentBytes,
			),
			maxTextAttachmentBytes: nonnegativeInteger(
				attachments.maxTextFileBytes,
				this.limitsValue.maxTextAttachmentBytes,
			),
			maxPromptAttachmentBytes: nonnegativeInteger(
				attachments.maxPromptBytes,
				this.limitsValue.maxPromptAttachmentBytes,
			),
			maxPromptTextBytes: nonnegativeInteger(
				attachments.maxPromptTextBytes,
				this.limitsValue.maxPromptTextBytes,
			),
			messagePageSize: positiveInteger(
				messages.maxPageSize,
				this.limitsValue.messagePageSize,
			),
			interactionPageSize: positiveInteger(
				interactions.defaultPageSize,
				this.limitsValue.interactionPageSize,
			),
			messageChunkBytes: positiveInteger(
				messageRecovery.maxChunkBytes,
				this.limitsValue.messageChunkBytes,
			),
			messageRecoveryBytes: positiveInteger(
				messageRecovery.maxTotalBytes,
				this.limitsValue.messageRecoveryBytes,
			),
			interactionChunkBytes: positiveInteger(
				interactionRecovery.maxChunkBytes,
				this.limitsValue.interactionChunkBytes,
			),
			interactionRecoveryBytes: positiveInteger(
				interactionRecovery.maxTotalBytes,
				this.limitsValue.interactionRecoveryBytes,
			),
		};
	}

	validateAttachments(
		existing: ReadonlyArray<{ file: File }>,
		files: readonly File[],
	): string | null {
		const limits = this.limitsValue;
		if (existing.length + files.length > limits.maxAttachmentsPerPrompt) {
			return `A prompt supports at most ${limits.maxAttachmentsPerPrompt} attachments`;
		}
		if (files.some((file) => file.size > limits.maxAttachmentBytes)) {
			return "An attachment exceeds the server file-size limit";
		}
		if (
			files.some(
				(file) =>
					!isRemoteImage(file) && file.size > limits.maxTextAttachmentBytes,
			)
		) {
			return "A text attachment exceeds the server size limit";
		}
		const allFiles = [...existing.map(({ file }) => file), ...files];
		const totalBytes = allFiles.reduce((sum, file) => sum + file.size, 0);
		if (totalBytes > limits.maxPromptAttachmentBytes) {
			return "Attachments exceed the server prompt-size limit";
		}
		const totalTextBytes = allFiles
			.filter((file) => !isRemoteImage(file))
			.reduce((sum, file) => sum + file.size, 0);
		return totalTextBytes > limits.maxPromptTextBytes
			? "Text attachments exceed the server prompt-size limit"
			: null;
	}

	async removeAttachment(id: string): Promise<void> {
		const response = await this.fetcher(
			`/api/attachments/${encodeURIComponent(id)}`,
			{ method: "DELETE" },
		);
		if (!response.ok && response.status !== 404) {
			throw new Error(`Attachment removal failed (${response.status})`);
		}
	}

	async uploadAttachment(file: File): Promise<string> {
		const form = new FormData();
		form.append("file", file);
		const response = await this.fetcher("/api/attachments", {
			method: "POST",
			body: form,
		});
		const payload: unknown = await response.json();
		if (!response.ok || !isRecord(payload) || !isRecord(payload.attachment)) {
			throw new Error(
				isRecord(payload) && typeof payload.error === "string"
					? payload.error
					: `Attachment upload failed (${response.status})`,
			);
		}
		if (typeof payload.attachment.id !== "string") {
			throw new Error("Attachment upload returned no id");
		}
		return payload.attachment.id;
	}

	async loadMessageRange(
		targetOffset: number,
		endOffset: number,
		initialTotalMessageCount: number,
	): Promise<MessageRange> {
		const messages: unknown[] = [];
		let cursor = targetOffset;
		let totalMessageCount = initialTotalMessageCount;
		while (cursor < endOffset) {
			const response = await this.rpc.command({
				type: "get_messages",
				offset: cursor,
				limit: Math.min(this.limitsValue.messagePageSize, endOffset - cursor),
			});
			if (!isRecord(response.data) || !Array.isArray(response.data.messages)) {
				throw new Error("Invalid message page response");
			}
			if (
				typeof response.data.offset === "number" &&
				response.data.offset !== cursor
			) {
				throw new Error("Message page is not contiguous");
			}
			if (response.data.messages.length === 0) {
				throw new Error("Message history ended before the requested cursor");
			}
			messages.push(
				...(await Promise.all(
					response.data.messages.map((message) =>
						this.resolveMessageReference(message),
					),
				)),
			);
			cursor += response.data.messages.length;
			totalMessageCount =
				typeof response.data.totalMessageCount === "number"
					? response.data.totalMessageCount
					: totalMessageCount;
		}
		return { messages, totalMessageCount };
	}

	async resolveMessageReference(message: unknown): Promise<unknown> {
		if (
			!isRecord(message) ||
			message.type !== "message_reference" ||
			typeof message.token !== "string"
		) {
			return message;
		}
		const chunks: Uint8Array[] = [];
		let offset = 0;
		let complete = false;
		while (!complete) {
			const response = await this.rpc.command({
				type: "get_message_chunk",
				token: message.token,
				offset,
				maxBytes: this.limitsValue.messageChunkBytes,
			});
			if (
				!isRecord(response.data) ||
				response.data.token !== message.token ||
				response.data.encoding !== "base64-json"
			) {
				throw new Error("Invalid message chunk response");
			}
			const chunk = decodeChunk(
				response.data,
				offset,
				this.limitsValue.messageRecoveryBytes,
				"Message",
			);
			chunks.push(chunk.bytes);
			offset = chunk.nextOffset;
			complete = chunk.complete;
		}
		return parseChunks(chunks);
	}

	async recoverMessageReference(
		message: Record<string, unknown>,
		messageIndex: number,
		canRebase: () => boolean,
	): Promise<RecoveredMessage> {
		let candidate: unknown = message;
		let lastError: unknown = null;
		let rebased = false;
		for (let attempt = 0; attempt < 3; attempt += 1) {
			try {
				return {
					message: await this.resolveMessageReference(candidate),
					rebased,
				};
			} catch (error) {
				lastError = error;
				if (!canRebase()) throw error;
				const response = await this.rpc.command({
					type: "get_messages",
					offset: messageIndex,
					limit: 1,
				});
				if (
					!isRecord(response.data) ||
					response.data.offset !== messageIndex ||
					!Array.isArray(response.data.messages) ||
					response.data.messages.length !== 1
				) {
					throw new Error("Message recovery returned the wrong record");
				}
				candidate = response.data.messages[0];
				rebased = true;
				if (!isRecord(candidate) || candidate.type !== "message_reference") {
					return { message: candidate, rebased };
				}
			}
		}
		return {
			message: {
				type: "message_unavailable",
				role: message.role,
				messageIndex,
				reason:
					lastError instanceof Error ? lastError.message : "recovery_failed",
			},
			rebased: true,
		};
	}

	async loadPendingInteractions(
		generation: number,
		initialTotalRequestCount: number,
		isCurrent: () => boolean,
	): Promise<PendingInteractionSet | null> {
		let offset = 0;
		let total = initialTotalRequestCount;
		const requests: unknown[] = [];
		while (offset < total) {
			const response = await this.rpc.command({
				type: "get_pending_interactions",
				offset,
				limit: Math.min(this.limitsValue.interactionPageSize, total - offset),
				generation,
			});
			if (!isCurrent()) return null;
			if (
				!isRecord(response.data) ||
				!Array.isArray(response.data.requests) ||
				typeof response.data.generation !== "number"
			) {
				throw new Error("Invalid pending interaction page response");
			}
			if (response.data.stale === true) return null;
			if (response.data.generation !== generation) {
				throw new Error("Pending interaction generation changed");
			}
			if (
				typeof response.data.offset === "number" &&
				response.data.offset !== offset
			) {
				throw new Error("Pending interaction page is not contiguous");
			}
			if (response.data.requests.length === 0) break;
			for (const request of response.data.requests) {
				if (
					!isRecord(request) ||
					typeof request.id !== "string" ||
					requests.some(
						(existing) => isRecord(existing) && existing.id === request.id,
					)
				) {
					continue;
				}
				requests.push(request);
			}
			offset += response.data.requests.length;
			total =
				typeof response.data.totalRequestCount === "number"
					? response.data.totalRequestCount
					: total;
		}
		return { requests, totalRequestCount: total };
	}

	async recoverInteraction(
		requestId: string,
	): Promise<Record<string, unknown>> {
		const chunks: Uint8Array[] = [];
		let offset = 0;
		let complete = false;
		while (!complete) {
			const response = await this.rpc.command({
				type: "get_pending_interaction_chunk",
				requestId,
				offset,
				maxBytes: this.limitsValue.interactionChunkBytes,
			});
			if (
				!isRecord(response.data) ||
				response.data.requestId !== requestId ||
				response.data.encoding !== "base64-json"
			) {
				throw new Error("Invalid interaction chunk response");
			}
			const chunk = decodeChunk(
				response.data,
				offset,
				this.limitsValue.interactionRecoveryBytes,
				"Interaction payload",
			);
			chunks.push(chunk.bytes);
			offset = chunk.nextOffset;
			complete = chunk.complete;
		}
		const hydrated = parseChunks(chunks);
		if (!isRecord(hydrated) || hydrated.id !== requestId) {
			throw new Error("Interaction recovery returned the wrong request");
		}
		return hydrated;
	}
}
