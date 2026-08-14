import type {
	CommandRegistry,
	TransportNeutralCommandContext,
} from "../features/commands";
import type {
	RemoteReviewNote,
	RemoteReviewService,
} from "../features/review/remote-service";
import type { ScratchpadController } from "../features/scratchpad/controller";
import type { MessagePart } from "../messages/parts";
import type { ThinkingLevel } from "../runtime/agent";
import type { AgentRuntime, AgentRuntimeEvent } from "../runtime/agent-runtime";
import { getAvailableThinkingLevels } from "../runtime/thinking-levels";
import {
	BUILT_IN_CHROME_CONTRIBUTION_IDS,
	type ChromeContribution,
	type ChromeContributionsController,
	type ChromeTextStyle,
} from "../shell/chrome-contributions";
import {
	MAX_REMOTE_ATTACHMENT_BYTES,
	MAX_REMOTE_ATTACHMENT_TOTAL_BYTES,
	MAX_REMOTE_ATTACHMENTS,
	MAX_REMOTE_ATTACHMENTS_PER_PROMPT,
	MAX_REMOTE_PROMPT_ATTACHMENT_BYTES,
	MAX_REMOTE_PROMPT_TEXT_BYTES,
	MAX_REMOTE_TEXT_ATTACHMENT_BYTES,
	type RemoteAttachmentStore,
} from "./remote-attachment-store";
import {
	REMOTE_INTERACTION_KINDS,
	type RemoteInteractionBroker,
} from "./remote-interaction-broker";
import { remoteMessagePreviews } from "./remote-message-queue";

export type RpcCommand = {
	id?: string;
	type: string;
	[key: string]: unknown;
};

export type RpcResponse = {
	id?: string;
	type: "response";
	command: string;
	success: boolean;
	data?: unknown;
	error?: string;
};

export type RpcWriter = (record: unknown) => Promise<void>;
export type RpcEventListener = (record: unknown) => void;

export type RpcConnectionSnapshot = {
	state: Record<string, unknown>;
	messages: unknown[];
	chrome?: RpcChromeSnapshot;
	messageOffset: number;
	totalMessageCount: number;
	pendingInteractions: unknown[];
	pendingInteractionGeneration: number;
};

export type RpcChromeSegment = {
	text: string;
	style?: Omit<ChromeTextStyle, "bg" | "fg">;
};

export type RpcChromeContribution = {
	id: string;
	content: RpcChromeSegment[];
	plainText: string;
	side: "left" | "right";
	action?: { type: "open-url"; url: string };
	clickable: boolean;
};

export type RpcChromeAreaSnapshot = {
	contributions: RpcChromeContribution[];
	hiddenBuiltinIds: string[];
};

export type RpcChromeSnapshot = {
	header: RpcChromeAreaSnapshot;
	footer: RpcChromeAreaSnapshot;
};

type ScheduledPrompt =
	| { kind: "message"; message: string }
	| {
			kind: "promptCommand";
			command: string;
			args: string;
			expandedPrompt: string;
	  };

export const RPC_PROTOCOL_VERSION = 2;

export const RPC_COMMAND_TYPES = [
	"prompt",
	"steer",
	"follow_up",
	"abort",
	"new_session",
	"list_sessions",
	"open_session",
	"change_cwd",
	"get_capabilities",
	"get_state",
	"get_messages",
	"get_last_assistant_text",
	"get_available_models",
	"set_model",
	"get_available_thinking_levels",
	"set_thinking_level",
	"get_scratchpad",
	"update_scratchpad",
] as const;

export type RpcSessionHostOptions = {
	persistSessions?: boolean;
	interactions?: RemoteInteractionBroker;
	attachments?: RemoteAttachmentStore;
	scratchpad?: ScratchpadController;
	review?: RemoteReviewService;
	commands?: CommandRegistry;
	header?: ChromeContributionsController;
	footer?: ChromeContributionsController;
	waitForWorkspaceReady?: () => Promise<void>;
	reloadHost?: (signal?: AbortSignal) => Promise<void>;
	allowLegacySessionPaths?: boolean;
	commandTimeoutMs?: number;
	commandCancellationGraceMs?: number;
};

const DEFAULT_COMMAND_TIMEOUT_MS = 30_000;
const DEFAULT_COMMAND_CANCELLATION_GRACE_MS = 2_000;
export const DEFAULT_RPC_MESSAGE_PAGE_SIZE = 100;
export const MAX_RPC_MESSAGE_PAGE_SIZE = 200;
export const DEFAULT_RPC_INTERACTION_PAGE_SIZE = 20;
export const MAX_RPC_INTERACTION_PAGE_SIZE = 50;
export const MAX_RPC_INTERACTION_CHUNK_BYTES = 48 * 1024;
export const MAX_RPC_INTERACTION_RECOVERY_BYTES = 2 * 1024 * 1024;

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requireString(command: RpcCommand, key: string): string {
	const value = command[key];
	if (typeof value !== "string" || !value.trim()) {
		throw new Error(`${key} must be a non-empty string`);
	}
	return value;
}

function requireText(command: RpcCommand, key: string): string {
	const value = command[key];
	if (typeof value !== "string") throw new Error(`${key} must be a string`);
	return value;
}

function requiredNonnegativeInteger(command: RpcCommand, key: string): number {
	const value = command[key];
	if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
		throw new Error(`${key} must be a non-negative integer`);
	}
	return value;
}

function optionalNonnegativeInteger(
	command: RpcCommand,
	key: string,
	fallback: number,
): number {
	const value = command[key];
	if (value === undefined) return fallback;
	if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
		throw new Error(`${key} must be a non-negative integer`);
	}
	return value;
}

const MAX_REMOTE_REVIEW_NOTES = 128;
const MAX_REMOTE_REVIEW_COMMENT_BYTES = 16 * 1024;
const MAX_REMOTE_REVIEW_TOTAL_COMMENT_BYTES = 192 * 1024;

function reviewSubmissionId(command: RpcCommand): string {
	const submissionId = requireString(command, "submissionId");
	if (submissionId.length > 128 || !/^[A-Za-z0-9._:-]+$/.test(submissionId)) {
		throw new Error("submissionId is invalid");
	}
	return submissionId;
}

function reviewNotes(command: RpcCommand): RemoteReviewNote[] {
	const value = command.notes;
	if (!Array.isArray(value) || value.length === 0) {
		throw new Error("notes must be a non-empty array");
	}
	if (value.length > MAX_REMOTE_REVIEW_NOTES) {
		throw new Error(`notes must not exceed ${MAX_REMOTE_REVIEW_NOTES} entries`);
	}
	let totalCommentBytes = 0;
	return value.map((item) => {
		if (!isRecord(item)) throw new Error("notes contains an invalid note");
		const path = item.path;
		const side = item.side;
		const startLine = item.startLine;
		const endLine = item.endLine;
		const comment = typeof item.comment === "string" ? item.comment.trim() : "";
		const commentBytes = Buffer.byteLength(comment, "utf8");
		totalCommentBytes += commentBytes;
		if (
			typeof path !== "string" ||
			!path ||
			path.length > 4_096 ||
			(side !== "additions" && side !== "deletions") ||
			typeof startLine !== "number" ||
			!Number.isSafeInteger(startLine) ||
			startLine < 1 ||
			typeof endLine !== "number" ||
			!Number.isSafeInteger(endLine) ||
			endLine < startLine ||
			!comment ||
			commentBytes > MAX_REMOTE_REVIEW_COMMENT_BYTES ||
			totalCommentBytes > MAX_REMOTE_REVIEW_TOTAL_COMMENT_BYTES
		) {
			throw new Error("notes contains an invalid note");
		}
		return { path, side, startLine, endLine, comment };
	});
}

function attachmentIds(command: RpcCommand): string[] {
	const value = command.attachmentIds;
	if (value === undefined) return [];
	if (
		!Array.isArray(value) ||
		!value.every((id) => typeof id === "string" && id.trim())
	) {
		throw new Error("attachmentIds must be an array of non-empty strings");
	}
	return value;
}

function jsonChunk(
	value: unknown,
	offset: number,
	maxBytes: number,
	maxTotalBytes: number,
) {
	const serialized = JSON.stringify(value, (_key, item) =>
		typeof item === "bigint" ? item.toString() : item,
	);
	if (serialized === undefined) throw new Error("Value cannot be serialized");
	const bytes = Buffer.from(serialized, "utf8");
	if (bytes.length > maxTotalBytes) {
		throw new Error(`Serialized value exceeds ${maxTotalBytes} bytes`);
	}
	if (offset > bytes.length) throw new Error("offset exceeds serialized data");
	const end = Math.min(bytes.length, offset + maxBytes);
	return {
		encoding: "base64-json" as const,
		data: bytes.subarray(offset, end).toString("base64"),
		offset,
		nextOffset: end,
		totalBytes: bytes.length,
		complete: end === bytes.length,
	};
}

function assistantText(message: unknown): string | null {
	if (!isRecord(message) || message.role !== "assistant") return null;
	if (!Array.isArray(message.content)) return null;
	const text = message.content
		.filter(
			(block): block is { type: "text"; text: string } =>
				isRecord(block) &&
				block.type === "text" &&
				typeof block.text === "string",
		)
		.map((block) => block.text)
		.join("\n");
	return text || null;
}

const MAX_REMOTE_CHROME_CONTRIBUTIONS = 64;
const MAX_REMOTE_CHROME_SEGMENTS = 32;
const MAX_REMOTE_CHROME_TEXT_LENGTH = 8_192;
const MAX_REMOTE_CHROME_ID_LENGTH = 256;
const MAX_REMOTE_CHROME_AREA_BYTES = 8 * 1024;

function hasAsciiControlCharacter(value: string): boolean {
	for (let index = 0; index < value.length; index += 1) {
		const code = value.charCodeAt(index);
		if (code < 32 || code === 127) return true;
	}
	return false;
}

function remoteChromeStyle(
	style: ChromeTextStyle | undefined,
): RpcChromeSegment["style"] {
	if (!style) return undefined;
	const projected: RpcChromeSegment["style"] = {};
	if (style.fgToken) projected.fgToken = style.fgToken;
	if (style.bgToken) projected.bgToken = style.bgToken;
	if (style.bold) projected.bold = true;
	if (style.dim) projected.dim = true;
	if (style.italic) projected.italic = true;
	if (style.underline) projected.underline = true;
	if (style.strikethrough) projected.strikethrough = true;
	return Object.keys(projected).length > 0 ? projected : undefined;
}

function remoteChromeContribution(
	contribution: ChromeContribution,
	remainingTextLength: number,
): { contribution: RpcChromeContribution; textLength: number } | null {
	if (
		!contribution.id ||
		contribution.id.length > MAX_REMOTE_CHROME_ID_LENGTH ||
		hasAsciiControlCharacter(contribution.id)
	) {
		return null;
	}
	let textLength = 0;
	const content: RpcChromeSegment[] = [];
	for (const segment of contribution.content.slice(
		0,
		MAX_REMOTE_CHROME_SEGMENTS,
	)) {
		const remaining = remainingTextLength - textLength;
		if (remaining <= 0) break;
		const text = segment.text.slice(0, remaining);
		if (!text) continue;
		textLength += text.length;
		const style = remoteChromeStyle(segment.style);
		content.push({ text, ...(style ? { style } : {}) });
	}
	const plainText = content.map((segment) => segment.text).join("");
	if (!plainText.trim()) return null;
	return {
		contribution: {
			id: contribution.id,
			content,
			plainText,
			side: contribution.side,
			...(contribution.action ? { action: contribution.action } : {}),
			clickable: contribution.onClick !== undefined,
		},
		textLength,
	};
}

function remoteChromeArea(
	controller: ChromeContributionsController | undefined,
	builtinIds: readonly string[],
): RpcChromeAreaSnapshot {
	if (!controller) return { contributions: [], hiddenBuiltinIds: [] };
	const contributions: RpcChromeContribution[] = [];
	const hiddenBuiltinIds = builtinIds.filter((id) => controller.isHidden(id));
	let remainingTextLength = MAX_REMOTE_CHROME_TEXT_LENGTH;
	for (const contribution of controller
		.getContributions()
		.slice(0, MAX_REMOTE_CHROME_CONTRIBUTIONS)) {
		const projected = remoteChromeContribution(
			contribution,
			remainingTextLength,
		);
		if (!projected) continue;
		const nextContributions = [...contributions, projected.contribution];
		if (
			Buffer.byteLength(
				JSON.stringify({
					contributions: nextContributions,
					hiddenBuiltinIds,
				}),
				"utf8",
			) > MAX_REMOTE_CHROME_AREA_BYTES
		) {
			break;
		}
		contributions.push(projected.contribution);
		remainingTextLength -= projected.textLength;
		if (remainingTextLength <= 0) break;
	}
	return { contributions, hiddenBuiltinIds };
}

export function rpcRecordsForRuntimeEvent(event: AgentRuntimeEvent): unknown[] {
	switch (event.type) {
		case "agent.start":
		case "agent.settled":
			return [{ type: event.type }];
		case "agent.end":
			return [
				{
					type: event.type,
					willRetry: event.willRetry ?? false,
				},
			];
		case "agent.turn.started":
			return [{ type: event.type, turnId: event.turn.id }];
		case "agent.turn.completed":
			return [{ type: event.type, turnId: event.turn?.id ?? null }];
		case "user.message.created":
		case "agent.message.started":
		case "agent.message.ended":
		case "session.message.appended":
			return [
				{
					type: event.type,
					turnId: event.turn.id,
					messageId: event.message.messageId,
					message: event.message,
				},
			];
		case "agent.message.updated":
			return [
				{
					type: event.type,
					turnId: event.turn.id,
					messageId: event.message.messageId,
					update: event.update,
				},
			];
		case "agent.tool.started":
			return [
				{
					type: event.type,
					turnId: event.turn.id,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
				},
			];
		case "agent.tool.updated":
			return [
				{
					type: event.type,
					turnId: event.turn.id,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					partialResult: event.partialResult,
				},
			];
		case "agent.tool.ended":
			return [
				{
					type: event.type,
					turnId: event.turn.id,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					result: event.result,
					isError: event.isError,
				},
			];
		case "chat.message-queue.changed":
			return [
				{
					type: event.type,
					steering: event.steering,
					followUp: event.messages,
					count: event.count,
					previews: remoteMessagePreviews(event.messages),
				},
			];
		case "chat.followups.promoted":
			return [{ type: event.type, count: event.count }];
		case "agent.retry.started":
			return [
				{
					type: event.type,
					attempt: event.attempt,
					maxAttempts: event.maxAttempts,
					delayMs: event.delayMs,
				},
			];
		case "agent.retry.failed":
			return [
				{
					type: event.type,
					attempt: event.attempt,
					error: event.error,
				},
			];
		case "agent.retry.completed":
			return [{ type: event.type, attempt: event.attempt }];
		case "agent.run.failed":
			return [{ type: event.type, error: event.error }];
		case "session.transcript.replaced":
			return [
				{
					type: event.type,
					reason: event.reason,
					removedMessageId: event.removedMessageId,
				},
			];
		case "session.compaction.completed.auto":
			return [
				{
					type: event.type,
					contextPercent: event.contextPercent,
					compactedTurnCount: event.compactedTurnCount,
					keptTurnCount: event.keptTurnCount,
				},
				{ type: "session.transcript.replaced", reason: "compaction" },
			];
		case "session.compaction.completed.recovery":
			return [
				{
					type: event.type,
					compactedTurnCount: event.compactedTurnCount,
					keptTurnCount: event.keptTurnCount,
				},
				{ type: "session.transcript.replaced", reason: "compaction" },
			];
		case "session.compaction.completed.adaptation":
			return [{ type: "session.transcript.replaced", reason: "compaction" }];
		case "session.compaction.completed.manual":
			return [
				{
					type: event.type,
					compactedTurnCount: event.compactedTurnCount,
					keptTurnCount: event.keptTurnCount,
				},
				{ type: "session.transcript.replaced", reason: "compaction" },
			];
		case "session.compaction.failed.auto":
		case "session.compaction.failed.recovery":
		case "session.compaction.failed.manual":
			return [{ type: event.type, error: event.error }];
		case "session.compaction.failed.adaptation":
			return [
				{
					type: event.type,
					modelId: event.modelId,
					modelName: event.modelName,
					cause: event.cause,
					error: event.error,
				},
			];
		case "session.handoff_summary.appended":
			return [
				{
					type: event.type,
					turnId: event.summaryMessage.turnId,
					messageId: event.summaryMessage.messageId,
					message: event.summaryMessage,
				},
			];
		default:
			return [];
	}
}

/**
 * Owns transport-independent RPC command dispatch and runtime event mapping.
 * Transports provide a response writer per command and subscribe to shared
 * runtime events independently.
 */
export class RpcSessionHost {
	private commandQueue = Promise.resolve();
	private readonly acceptedRuns = new Set<Promise<void>>();
	private readonly listeners = new Set<RpcEventListener>();
	private readonly pendingEvents: unknown[] = [];
	private readonly unsubscribeRuntime: () => void;
	private readonly unsubscribeInteractions: (() => void) | null;
	private readonly unsubscribeScratchpad: (() => void) | null;
	private readonly unsubscribeReview: (() => void) | null;
	private readonly unsubscribeCommands: (() => void) | null;
	private readonly unsubscribeHeader: (() => void) | null;
	private readonly unsubscribeFooter: (() => void) | null;
	private readonly persistSessions: boolean;
	private readonly interactions?: RemoteInteractionBroker;
	private readonly attachments?: RemoteAttachmentStore;
	private readonly scratchpad?: ScratchpadController;
	private readonly review?: RemoteReviewService;
	private readonly commands?: CommandRegistry;
	private readonly header?: ChromeContributionsController;
	private readonly footer?: ChromeContributionsController;
	private readonly waitForWorkspaceReady: () => Promise<void>;
	private readonly reloadHost?: (signal?: AbortSignal) => Promise<void>;
	private readonly allowLegacySessionPaths: boolean;
	private readonly commandTimeoutMs: number;
	private readonly commandCancellationGraceMs: number;
	private activeCommandAbort: AbortController | null = null;
	private activeCommandExecution: Promise<void> | null = null;
	private commandGeneration = 0;
	private commandRegistryGeneration = 0;
	private commandExecutionCompromised = false;
	private promptReserved = false;
	private acceptingCommands = true;
	private disposed = false;

	constructor(
		private readonly runtime: AgentRuntime,
		options: RpcSessionHostOptions = {},
	) {
		this.persistSessions = options.persistSessions ?? false;
		this.interactions = options.interactions;
		this.attachments = options.attachments;
		this.scratchpad = options.scratchpad;
		this.review = options.review;
		this.commands = options.commands;
		this.header = options.header;
		this.footer = options.footer;
		this.waitForWorkspaceReady =
			options.waitForWorkspaceReady ?? (async () => {});
		this.reloadHost = options.reloadHost;
		this.allowLegacySessionPaths = options.allowLegacySessionPaths ?? true;
		this.commandTimeoutMs =
			options.commandTimeoutMs ?? DEFAULT_COMMAND_TIMEOUT_MS;
		this.commandCancellationGraceMs =
			options.commandCancellationGraceMs ??
			DEFAULT_COMMAND_CANCELLATION_GRACE_MS;
		this.unsubscribeRuntime = runtime.subscribe((event) => {
			for (const record of rpcRecordsForRuntimeEvent(event)) {
				this.publish(record);
			}
			if (
				event.type === "agent.turn.completed" ||
				event.type === "session.active.changed" ||
				event.type === "session.model.changed" ||
				event.type === "session.thinking_level.changed" ||
				event.type.startsWith("session.compaction.completed.")
			) {
				this.publishStateChanged();
			}
		});
		this.unsubscribeInteractions =
			this.interactions?.subscribe((event) => this.publish(event)) ?? null;
		this.unsubscribeScratchpad =
			this.scratchpad?.subscribe((snapshot) =>
				this.publish({ type: "scratchpad.changed", ...snapshot }),
			) ?? null;
		this.unsubscribeReview =
			this.review?.subscribe(() => this.publish({ type: "review.changed" })) ??
			null;
		this.unsubscribeCommands =
			this.commands?.subscribe(() => {
				this.commandRegistryGeneration += 1;
			}) ?? null;
		const publishChromeChanged = () =>
			this.publish({
				type: "shell.chrome.changed",
				chrome: this.chromeSnapshot(),
			});
		this.unsubscribeHeader =
			this.header?.subscribe(publishChromeChanged) ?? null;
		this.unsubscribeFooter =
			this.footer?.subscribe(publishChromeChanged) ?? null;
	}

	subscribe(listener: RpcEventListener): () => void {
		if (this.disposed) return () => {};
		this.listeners.add(listener);
		const pending = this.pendingEvents.splice(0);
		for (const record of pending) this.notifyListener(listener, record);
		return () => this.listeners.delete(listener);
	}

	handleCommand(command: RpcCommand, respond: RpcWriter): Promise<void> {
		if (this.disposed) {
			return respond(
				this.response(command, false, undefined, "RPC host is disposed"),
			);
		}
		if (command.type === "ui_response") {
			return this.handleInteractionResponse(command, respond);
		}
		if (command.type === "abort") {
			return this.handleAbort(command, respond);
		}
		const generation = this.commandGeneration;
		const operation = this.commandQueue.then(async () => {
			if (!this.acceptingCommands) {
				await respond(
					this.response(command, false, undefined, "RPC host is shutting down"),
				);
				return;
			}
			if (generation !== this.commandGeneration) {
				await respond(
					this.response(
						command,
						false,
						undefined,
						"Command cancelled by abort",
					),
				);
				return;
			}
			if (this.commandExecutionCompromised) {
				await respond(
					this.response(
						command,
						false,
						undefined,
						"A cancelled command did not stop; restart the RPC host",
					),
				);
				return;
			}
			try {
				await this.dispatch(command, respond);
			} catch (error) {
				await respond(this.response(command, false, undefined, error));
			}
		});
		this.commandQueue = operation.catch(() => {});
		return operation;
	}

	connectClient(listener: RpcEventListener): () => void {
		if (!this.interactions || this.disposed) return () => {};
		for (const event of this.interactions.connectClient()) listener(event);
		return () => {};
	}

	getConnectionSnapshot(
		maxMessages = Number.MAX_SAFE_INTEGER,
	): RpcConnectionSnapshot {
		const allMessages = this.runtime.getMessages();
		const count = Number.isSafeInteger(maxMessages)
			? Math.max(0, maxMessages)
			: 0;
		const messageOffset = Math.max(0, allMessages.length - count);
		const pending = this.interactions?.getPendingSnapshot() ?? {
			generation: 0,
			requests: [],
		};
		return {
			state: this.stateSnapshot(),
			messages: allMessages.slice(messageOffset),
			chrome: this.chromeSnapshot(),
			messageOffset,
			totalMessageCount: allMessages.length,
			pendingInteractions: pending.requests,
			pendingInteractionGeneration: pending.generation,
		};
	}

	async waitForCommands(): Promise<void> {
		await this.commandQueue;
	}

	async abortAndWait(): Promise<void> {
		this.acceptingCommands = false;
		this.commandGeneration += 1;
		this.interactions?.dispose();
		this.activeCommandAbort?.abort(new Error("Command execution aborted"));
		this.runtime.abort();
		await this.commandQueue;
		await Promise.allSettled(this.acceptedRuns);
	}

	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		this.unsubscribeRuntime();
		this.unsubscribeInteractions?.();
		this.unsubscribeScratchpad?.();
		this.unsubscribeReview?.();
		this.unsubscribeCommands?.();
		this.unsubscribeHeader?.();
		this.unsubscribeFooter?.();
		this.interactions?.dispose();
		this.attachments?.dispose();
		this.listeners.clear();
		this.pendingEvents.length = 0;
	}

	private publish(record: unknown): void {
		if (this.listeners.size === 0) {
			this.pendingEvents.push(record);
			if (this.pendingEvents.length > 64) this.pendingEvents.shift();
			return;
		}
		for (const listener of [...this.listeners]) {
			this.notifyListener(listener, record);
		}
	}

	private notifyListener(listener: RpcEventListener, record: unknown): void {
		try {
			listener(record);
		} catch (error) {
			console.error(
				`RPC event listener failed: ${error instanceof Error ? error.message : String(error)}`,
			);
		}
	}

	private chromeSnapshot(): RpcChromeSnapshot {
		return {
			header: remoteChromeArea(this.header, [
				BUILT_IN_CHROME_CONTRIBUTION_IDS.headerTitle,
				BUILT_IN_CHROME_CONTRIBUTION_IDS.headerModel,
				BUILT_IN_CHROME_CONTRIBUTION_IDS.headerThinking,
				BUILT_IN_CHROME_CONTRIBUTION_IDS.headerUpdate,
			]),
			footer: remoteChromeArea(this.footer, [
				BUILT_IN_CHROME_CONTRIBUTION_IDS.footerLocation,
			]),
		};
	}

	private stateSnapshot(): Record<string, unknown> {
		const session = this.runtime.getSession();
		const status = this.runtime.getStatus();
		const contextUsage = status.contextUsage;
		return {
			model: this.runtime.getCurrentModel(),
			thinkingLevel: this.runtime.agentInfo.thinkingLevel,
			isStreaming: status.isStreaming,
			contextUsage: contextUsage
				? {
						tokens: contextUsage.tokens,
						contextWindow: contextUsage.contextWindow,
						percent: contextUsage.percent,
					}
				: null,
			sessionId: session.id,
			sessionName: session.name,
			cwd: session.cwd,
			messageCount: this.runtime.getMessages().length,
			pendingMessageCount: this.runtime.getPendingMessageCount(),
			pendingMessagePreviews: remoteMessagePreviews(
				this.runtime.getPendingMessages(),
			),
		};
	}

	private publishStateChanged(): void {
		this.publish({ type: "state_changed", state: this.stateSnapshot() });
	}

	private async handleInteractionResponse(
		command: RpcCommand,
		respond: RpcWriter,
	): Promise<void> {
		try {
			if (!this.acceptingCommands) throw new Error("RPC host is shutting down");
			if (!this.interactions) throw new Error("Interactive UI is unavailable");
			const result = this.interactions.respond(
				requireString(command, "requestId"),
				command.response,
			);
			if (!result.accepted) throw new Error(result.error);
			await respond(this.response(command, true));
		} catch (error) {
			await respond(this.response(command, false, undefined, error));
		}
	}

	private async handleAbort(
		command: RpcCommand,
		respond: RpcWriter,
	): Promise<void> {
		try {
			const queuedBeforeAbort = this.commandQueue;
			this.commandGeneration += 1;
			this.activeCommandAbort?.abort(new Error("Command execution aborted"));
			this.runtime.abort();
			await Promise.allSettled([
				...this.acceptedRuns,
				...(this.activeCommandExecution ? [this.activeCommandExecution] : []),
			]);
			await queuedBeforeAbort;
			await respond(this.response(command, true));
		} catch (error) {
			await respond(this.response(command, false, undefined, error));
		}
	}

	private async executeTransportNeutralCommand(
		handler: (ctx: TransportNeutralCommandContext) => void | Promise<void>,
		args: string,
		options: {
			timeoutMs?: number | null;
			cancellation?: "grace" | "settle";
		} = {},
	): Promise<ScheduledPrompt | null> {
		const abortController = new AbortController();
		let scheduledPrompt: ScheduledPrompt | null = null;
		const timeoutMs =
			options.timeoutMs === undefined
				? this.commandTimeoutMs
				: options.timeoutMs;
		const timeout =
			timeoutMs === null
				? null
				: setTimeout(() => {
						abortController.abort(new Error("Command execution timed out"));
					}, timeoutMs);
		const schedule = (prompt: ScheduledPrompt) => {
			if (scheduledPrompt !== null) {
				throw new Error("Command scheduled more than one prompt");
			}
			scheduledPrompt = prompt;
		};
		const handlerExecution = Promise.resolve().then(() =>
			handler({
				runtime: this.runtime,
				args,
				persistSessions: this.persistSessions,
				schedulePrompt: (message) => {
					const prompt = message.trim();
					if (!prompt) throw new Error("Scheduled prompt must not be empty");
					schedule({ kind: "message", message: prompt });
				},
				schedulePromptCommand: (command, commandArgs, expandedPrompt) => {
					const commandName = command.trim();
					const prompt = expandedPrompt.trim();
					if (!commandName) {
						throw new Error("Scheduled prompt command must not be empty");
					}
					if (!prompt) throw new Error("Scheduled prompt must not be empty");
					schedule({
						kind: "promptCommand",
						command: commandName,
						args: commandArgs,
						expandedPrompt: prompt,
					});
				},
				reloadHost: async (signal) => {
					if (!this.reloadHost) throw new Error("Host reload is unavailable");
					await this.reloadHost(signal);
				},
				signal: abortController.signal,
			}),
		);
		const execution = new Promise<void>((resolve, reject) => {
			const handleAbort = () => {
				reject(
					abortController.signal.reason instanceof Error
						? abortController.signal.reason
						: new Error("Command execution aborted"),
				);
			};
			abortController.signal.addEventListener("abort", handleAbort, {
				once: true,
			});
			handlerExecution.then(resolve, reject).finally(() => {
				abortController.signal.removeEventListener("abort", handleAbort);
			});
		});
		this.activeCommandAbort = abortController;
		this.activeCommandExecution = execution;
		try {
			await execution;
			return scheduledPrompt;
		} catch (error) {
			if (abortController.signal.aborted) {
				if (options.cancellation === "settle") {
					await handlerExecution.catch(() => {});
				} else if (
					!(await this.waitForCommandHandlerToStop(handlerExecution))
				) {
					this.commandExecutionCompromised = true;
				}
			}
			throw error;
		} finally {
			if (timeout !== null) clearTimeout(timeout);
			if (this.activeCommandAbort === abortController) {
				this.activeCommandAbort = null;
				this.activeCommandExecution = null;
			}
		}
	}

	private waitForCommandHandlerToStop(
		handlerExecution: Promise<unknown>,
	): Promise<boolean> {
		return new Promise((resolve) => {
			const timeout = setTimeout(
				() => resolve(false),
				this.commandCancellationGraceMs,
			);
			handlerExecution.then(
				() => {
					clearTimeout(timeout);
					resolve(true);
				},
				() => {
					clearTimeout(timeout);
					resolve(true);
				},
			);
		});
	}

	private async dispatch(
		command: RpcCommand,
		respond: RpcWriter,
	): Promise<void> {
		switch (command.type) {
			case "prompt": {
				const ids = attachmentIds(command);
				const messageValue = command.message;
				if (messageValue !== undefined && typeof messageValue !== "string") {
					throw new Error("message must be a string");
				}
				const message = messageValue ?? "";
				if (!message.trim() && ids.length === 0) {
					throw new Error("message or attachmentIds is required");
				}
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					if (ids.length > 0) {
						throw new Error("Attachments cannot be submitted while streaming");
					}
					if (command.streamingBehavior === "steer") {
						this.runtime.sendSteer(message);
					} else if (command.streamingBehavior === "followUp") {
						this.runtime.sendFollowUp(message);
					} else {
						throw new Error(
							'Agent is streaming; set streamingBehavior to "steer" or "followUp"',
						);
					}
					await respond(this.response(command, true));
					return;
				}
				if (ids.length > 0 && !this.attachments) {
					throw new Error("Attachments are unavailable");
				}
				const claim = ids.length > 0 ? this.attachments?.claim(ids) : undefined;
				const parts: MessagePart[] = [
					...(message.trim() ? [{ type: "text" as const, text: message }] : []),
					...(claim?.parts ?? []),
				];
				const input = ids.length > 0 ? parts : message;
				this.promptReserved = true;
				const acceptanceGeneration = this.commandGeneration;
				try {
					await respond(this.response(command, true));
				} catch (error) {
					this.promptReserved = false;
					claim?.release();
					throw error;
				}
				if (
					!this.acceptingCommands ||
					acceptanceGeneration !== this.commandGeneration
				) {
					this.promptReserved = false;
					claim?.release();
					return;
				}
				let run: Promise<void>;
				run = Promise.resolve()
					.then(() =>
						this.runtime.submitUserMessage(input, () => claim?.commit()),
					)
					.catch((error) => {
						claim?.release();
						this.publish({
							type: "error",
							error: error instanceof Error ? error.message : String(error),
						});
					})
					.finally(() => {
						this.promptReserved = false;
						this.acceptedRuns.delete(run);
					});
				this.acceptedRuns.add(run);
				return;
			}
			case "steer":
				this.runtime.sendSteer(requireString(command, "message"));
				await respond(this.response(command, true));
				return;
			case "follow_up":
				this.runtime.sendFollowUp(requireString(command, "message"));
				await respond(this.response(command, true));
				return;
			case "new_session":
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot create a session while the agent is streaming",
					);
				}
				await this.runtime.newSession(undefined, {
					persist: this.persistSessions,
				});
				await this.runtime.waitForModelAdaptation();
				await this.waitForWorkspaceReady();
				await respond(
					this.response(command, true, {
						cancelled: false,
					}),
				);
				return;
			case "list_sessions": {
				const cwd = command.cwd;
				if (cwd !== undefined && typeof cwd !== "string") {
					throw new Error("cwd must be a string");
				}
				const sessions = cwd
					? await this.runtime.listSessionsForCwd(cwd)
					: await this.runtime.listAllSessions();
				await respond(this.response(command, true, { sessions }));
				return;
			}
			case "open_session": {
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot switch sessions while the agent is streaming",
					);
				}
				const switched = await this.runtime.switchSessionById(
					requireString(command, "sessionId"),
				);
				if (!switched) throw new Error("Session not found");
				await this.runtime.waitForModelAdaptation();
				await this.waitForWorkspaceReady();
				await respond(this.response(command, true, this.stateSnapshot()));
				return;
			}
			case "change_cwd": {
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot change the working directory while the agent is streaming",
					);
				}
				await this.runtime.changeCwd(requireString(command, "cwd"));
				await this.waitForWorkspaceReady();
				await respond(this.response(command, true, this.stateSnapshot()));
				return;
			}
			case "activate_chrome_contribution": {
				const area = requireString(command, "area");
				if (area !== "header" && area !== "footer") {
					throw new Error("area must be header or footer");
				}
				const contributionId = requireString(command, "contributionId");
				const controller = area === "header" ? this.header : this.footer;
				if (!controller)
					throw new Error(`${area} contributions are unavailable`);
				let activated = false;
				await this.executeTransportNeutralCommand(async ({ signal }) => {
					activated = await controller.activateContribution(
						contributionId,
						signal,
					);
				}, "");
				if (!activated) {
					throw new Error("Chrome contribution is no longer actionable");
				}
				await respond(this.response(command, true));
				return;
			}
			case "list_commands": {
				if (!this.commands) throw new Error("Command registry is unavailable");
				const seenCommandIds = new Set<string>();
				const commands = this.commands
					.getAll()
					.filter((candidate) => {
						if (seenCommandIds.has(candidate.name)) return false;
						seenCommandIds.add(candidate.name);
						return candidate.executeTransportNeutral !== undefined;
					})
					.map((candidate) => ({
						id: candidate.name,
						name: candidate.displayName ?? candidate.name,
						description: candidate.description,
						argName: candidate.argName,
						category: candidate.category,
					}));
				await respond(
					this.response(command, true, {
						commands,
						registryGeneration: this.commandRegistryGeneration,
					}),
				);
				return;
			}
			case "execute_command": {
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot execute commands while the agent is streaming",
					);
				}
				if (!this.commands) throw new Error("Command registry is unavailable");
				if (command.registryGeneration !== undefined) {
					const registryGeneration = optionalNonnegativeInteger(
						command,
						"registryGeneration",
						this.commandRegistryGeneration,
					);
					if (registryGeneration !== this.commandRegistryGeneration) {
						throw new Error("Command registry changed; refresh commands");
					}
				}
				const commandId = requireString(command, "commandId");
				const args = command.args;
				if (args !== undefined && typeof args !== "string") {
					throw new Error("args must be a string");
				}
				if (command.expectedSessionId !== undefined) {
					const expectedSessionId = requireString(command, "expectedSessionId");
					if (this.runtime.getSession().id !== expectedSessionId) {
						throw new Error("Active session changed; retry the command");
					}
				}
				const registered = this.commands
					.getAll()
					.find((candidate) => candidate.name === commandId);
				if (!registered) throw new Error(`Command not found: ${commandId}`);
				if (!registered.executeTransportNeutral) {
					throw new Error(`Command is not available remotely: ${commandId}`);
				}
				const executionGeneration = this.commandGeneration;
				const scheduledPrompt = await this.executeTransportNeutralCommand(
					registered.executeTransportNeutral,
					args ?? "",
					{
						timeoutMs: registered.transportNeutralTimeoutMs,
						cancellation: registered.transportNeutralCancellation,
					},
				);
				await this.waitForWorkspaceReady();
				if (executionGeneration !== this.commandGeneration) {
					throw new Error("Command cancelled by abort");
				}
				if (!scheduledPrompt) {
					await respond(this.response(command, true));
					return;
				}
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Command cannot schedule a prompt while the agent is running",
					);
				}
				this.promptReserved = true;
				try {
					await respond(this.response(command, true));
				} catch (error) {
					this.promptReserved = false;
					throw error;
				}
				if (
					!this.acceptingCommands ||
					executionGeneration !== this.commandGeneration
				) {
					this.promptReserved = false;
					return;
				}
				let run: Promise<void>;
				run = Promise.resolve()
					.then(() =>
						scheduledPrompt.kind === "message"
							? this.runtime.submitUserMessage(scheduledPrompt.message)
							: this.runtime.submitPromptCommandMessage(
									scheduledPrompt.command,
									scheduledPrompt.args,
									scheduledPrompt.expandedPrompt,
								),
					)
					.catch((error) => {
						this.publish({
							type: "error",
							error: error instanceof Error ? error.message : String(error),
						});
					})
					.finally(() => {
						this.promptReserved = false;
						this.acceptedRuns.delete(run);
					});
				this.acceptedRuns.add(run);
				return;
			}
			case "get_capabilities":
				await respond(
					this.response(command, true, {
						protocolVersion: RPC_PROTOCOL_VERSION,
						commands: [
							...RPC_COMMAND_TYPES,
							...(this.allowLegacySessionPaths ? ["switch_session"] : []),
							...(this.interactions
								? [
										"ui_response",
										"get_pending_interactions",
										"get_pending_interaction_chunk",
									]
								: []),
							...(this.commands ? ["list_commands", "execute_command"] : []),
							...(this.header || this.footer
								? ["activate_chrome_contribution"]
								: []),
							...(this.scratchpad
								? ["get_scratchpad", "update_scratchpad"]
								: []),
							...(this.review
								? ["get_review_state", "get_review_file", "submit_review"]
								: []),
						],
						interactiveUI: this.interactions !== undefined,
						chromeContributions:
							this.header !== undefined || this.footer !== undefined,
						interactionKinds:
							this.interactions === undefined ? [] : REMOTE_INTERACTION_KINDS,
						attachmentReferences: this.attachments !== undefined,
						maxAttachmentsPerPrompt: this.attachments
							? MAX_REMOTE_ATTACHMENTS_PER_PROMPT
							: 0,
						limits: {
							attachments: {
								maxFiles: this.attachments ? MAX_REMOTE_ATTACHMENTS : 0,
								maxFilesPerPrompt: this.attachments
									? MAX_REMOTE_ATTACHMENTS_PER_PROMPT
									: 0,
								maxFileBytes: this.attachments
									? MAX_REMOTE_ATTACHMENT_BYTES
									: 0,
								maxTextFileBytes: this.attachments
									? MAX_REMOTE_TEXT_ATTACHMENT_BYTES
									: 0,
								maxTotalBytes: this.attachments
									? MAX_REMOTE_ATTACHMENT_TOTAL_BYTES
									: 0,
								maxPromptBytes: this.attachments
									? MAX_REMOTE_PROMPT_ATTACHMENT_BYTES
									: 0,
								maxPromptTextBytes: this.attachments
									? MAX_REMOTE_PROMPT_TEXT_BYTES
									: 0,
								maxConcurrentUploads: 0,
							},
							pagination: {
								messages: {
									defaultPageSize: DEFAULT_RPC_MESSAGE_PAGE_SIZE,
									maxPageSize: MAX_RPC_MESSAGE_PAGE_SIZE,
								},
								pendingInteractions: {
									defaultPageSize: DEFAULT_RPC_INTERACTION_PAGE_SIZE,
									maxPageSize: MAX_RPC_INTERACTION_PAGE_SIZE,
								},
							},
							recovery: {
								pendingInteraction: {
									maxChunkBytes: MAX_RPC_INTERACTION_CHUNK_BYTES,
									maxTotalBytes: MAX_RPC_INTERACTION_RECOVERY_BYTES,
								},
							},
						},
						eventSequencing: { supported: false },
					}),
				);
				return;
			case "get_state":
				await respond(this.response(command, true, this.stateSnapshot()));
				return;
			case "get_scratchpad": {
				if (!this.scratchpad) throw new Error("Scratchpad is unavailable");
				await respond(
					this.response(command, true, {
						sessionId: this.scratchpad.sessionId(),
						content: this.scratchpad.content(),
					}),
				);
				return;
			}
			case "update_scratchpad": {
				if (!this.scratchpad) throw new Error("Scratchpad is unavailable");
				const sessionId = requireString(command, "sessionId");
				if (sessionId !== this.runtime.getSession().id) {
					throw new Error("Active session changed; reload the scratchpad");
				}
				const expectedContent = requireText(command, "expectedContent");
				const content = requireText(command, "content");
				const result = this.scratchpad.applyAtomicUpdate(
					sessionId,
					(current) => (current === expectedContent ? content : null),
				);
				if (!result)
					throw new Error("Active session changed; retry the update");
				if (!result.updated && result.content !== content) {
					throw new Error(
						"Scratchpad changed elsewhere; reload before editing",
					);
				}
				await respond(
					this.response(command, true, { sessionId, content: result.content }),
				);
				return;
			}
			case "submit_review": {
				const review = this.review;
				if (!review) throw new Error("Code review is unavailable");
				const sessionId = requireString(command, "sessionId");
				const generation = requiredNonnegativeInteger(command, "generation");
				const dispatchGeneration = this.commandGeneration;
				const submission = await review.prepareSubmission(
					reviewSubmissionId(command),
					sessionId,
					generation,
					reviewNotes(command),
				);
				if (dispatchGeneration !== this.commandGeneration) {
					throw new Error("Command cancelled by abort");
				}
				if (!submission.part) {
					await respond(this.response(command, true, { duplicate: true }));
					return;
				}
				const part = submission.part;
				if (
					!this.acceptingCommands ||
					this.promptReserved ||
					this.runtime.getStatus().isStreaming
				) {
					throw new Error(
						"Cannot submit a review while the agent is streaming",
					);
				}
				review.assertCurrent(sessionId, generation);
				this.promptReserved = true;

				let accepted = false;
				let resolveAcceptance: () => void = () => {};
				let rejectAcceptance: (error: Error) => void = () => {};
				const acceptance = new Promise<void>((resolve, reject) => {
					resolveAcceptance = resolve;
					rejectAcceptance = reject;
				});
				let run: Promise<void>;
				run = Promise.resolve()
					.then(() => {
						if (
							!this.acceptingCommands ||
							dispatchGeneration !== this.commandGeneration
						) {
							throw new Error("Command cancelled by abort");
						}
						if (this.runtime.getStatus().isStreaming) {
							throw new Error(
								"Cannot submit a review while the agent is streaming",
							);
						}
						review.assertCurrent(sessionId, generation);
						return this.runtime.submitUserMessage([part], () => {
							review.markSubmissionAccepted(submission);
							accepted = true;
							resolveAcceptance();
						});
					})
					.catch((error) => {
						const normalized =
							error instanceof Error ? error : new Error(String(error));
						if (!accepted) rejectAcceptance(normalized);
						else this.publish({ type: "error", error: normalized.message });
					})
					.finally(() => {
						this.promptReserved = false;
						this.acceptedRuns.delete(run);
					});
				this.acceptedRuns.add(run);
				await acceptance;
				await respond(this.response(command, true));
				return;
			}
			case "get_review_state": {
				if (!this.review) throw new Error("Code review is unavailable");
				await respond(
					this.response(command, true, await this.review.refresh()),
				);
				return;
			}
			case "get_review_file": {
				if (!this.review) throw new Error("Code review is unavailable");
				const state = await this.review.refresh();
				await respond(
					this.response(
						command,
						true,
						this.review.getFile(
							state.sessionId,
							requireString(command, "path"),
						),
					),
				);
				return;
			}
			case "get_messages": {
				const messages = this.runtime.getMessages();
				const paginated =
					command.offset !== undefined || command.limit !== undefined;
				if (!paginated) {
					await respond(this.response(command, true, { messages }));
					return;
				}
				const offset = optionalNonnegativeInteger(command, "offset", 0);
				const limit = optionalNonnegativeInteger(
					command,
					"limit",
					DEFAULT_RPC_MESSAGE_PAGE_SIZE,
				);
				if (limit > MAX_RPC_MESSAGE_PAGE_SIZE) {
					throw new Error(`limit must not exceed ${MAX_RPC_MESSAGE_PAGE_SIZE}`);
				}
				const page = messages.slice(offset, offset + limit);
				await respond(
					this.response(command, true, {
						messages: page,
						offset,
						totalMessageCount: messages.length,
						hasMore: offset + page.length < messages.length,
					}),
				);
				return;
			}
			case "get_pending_interactions": {
				if (!this.interactions)
					throw new Error("Interactive UI is unavailable");
				const pending = this.interactions.getPendingSnapshot();
				const offset = optionalNonnegativeInteger(command, "offset", 0);
				const limit = optionalNonnegativeInteger(
					command,
					"limit",
					DEFAULT_RPC_INTERACTION_PAGE_SIZE,
				);
				if (limit > MAX_RPC_INTERACTION_PAGE_SIZE) {
					throw new Error(
						`limit must not exceed ${MAX_RPC_INTERACTION_PAGE_SIZE}`,
					);
				}
				const expectedGeneration = command.generation;
				if (
					expectedGeneration !== undefined &&
					(typeof expectedGeneration !== "number" ||
						!Number.isSafeInteger(expectedGeneration) ||
						expectedGeneration < 0)
				) {
					throw new Error("generation must be a non-negative integer");
				}
				const stale =
					typeof expectedGeneration === "number" &&
					expectedGeneration !== pending.generation;
				const requests = stale
					? []
					: pending.requests.slice(offset, offset + limit);
				await respond(
					this.response(command, true, {
						requests,
						offset,
						generation: pending.generation,
						stale,
						totalRequestCount: pending.requests.length,
						hasMore:
							!stale && offset + requests.length < pending.requests.length,
					}),
				);
				return;
			}
			case "get_pending_interaction_chunk": {
				if (!this.interactions)
					throw new Error("Interactive UI is unavailable");
				const requestId = requireString(command, "requestId");
				const pending = this.interactions.getPendingSnapshot();
				const request = pending.requests.find(
					(candidate) => candidate.id === requestId,
				);
				if (!request) throw new Error("Interaction is no longer pending");
				const offset = optionalNonnegativeInteger(command, "offset", 0);
				const maxBytes = optionalNonnegativeInteger(
					command,
					"maxBytes",
					MAX_RPC_INTERACTION_CHUNK_BYTES,
				);
				if (maxBytes < 1 || maxBytes > MAX_RPC_INTERACTION_CHUNK_BYTES) {
					throw new Error(
						`maxBytes must be between 1 and ${MAX_RPC_INTERACTION_CHUNK_BYTES}`,
					);
				}
				await respond(
					this.response(command, true, {
						requestId,
						generation: pending.generation,
						...jsonChunk(
							request,
							offset,
							maxBytes,
							MAX_RPC_INTERACTION_RECOVERY_BYTES,
						),
					}),
				);
				return;
			}
			case "get_last_assistant_text": {
				const message = this.runtime
					.getMessages()
					.findLast((candidate) => candidate.role === "assistant");
				await respond(
					this.response(command, true, {
						text: assistantText(message),
					}),
				);
				return;
			}
			case "get_available_models":
				await respond(
					this.response(command, true, {
						models: this.runtime.getAvailableModels(),
					}),
				);
				return;
			case "set_model": {
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error("Cannot change models while the agent is streaming");
				}
				const provider = requireString(command, "provider");
				const modelId = requireString(command, "modelId");
				const model = this.runtime
					.getAvailableModels()
					.find(
						(candidate) =>
							candidate.provider === provider && candidate.id === modelId,
					);
				if (!model) throw new Error(`Model not found: ${provider}/${modelId}`);
				this.runtime.setModel(model);
				await this.runtime.waitForModelAdaptation();
				await respond(this.response(command, true, model));
				return;
			}
			case "get_available_thinking_levels":
				await respond(
					this.response(command, true, {
						levels: getAvailableThinkingLevels(this.runtime.getCurrentModel()),
					}),
				);
				return;
			case "set_thinking_level": {
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot change thinking level while the agent is streaming",
					);
				}
				const level = requireString(command, "level") as ThinkingLevel;
				const levels = getAvailableThinkingLevels(
					this.runtime.getCurrentModel(),
				);
				if (!levels.includes(level)) {
					throw new Error(`Thinking level not available: ${level}`);
				}
				this.runtime.setThinkingLevel(level);
				await respond(this.response(command, true));
				return;
			}
			case "switch_session": {
				if (!this.allowLegacySessionPaths) {
					throw new Error("Legacy session paths are unavailable");
				}
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot switch sessions while the agent is streaming",
					);
				}
				const switched = await this.runtime.switchSession(
					requireString(command, "sessionPath"),
				);
				if (!switched) throw new Error("Session not found");
				await this.runtime.waitForModelAdaptation();
				await this.waitForWorkspaceReady();
				await respond(
					this.response(command, true, {
						cancelled: false,
					}),
				);
				return;
			}
			default:
				throw new Error(`Unknown command: ${command.type}`);
		}
	}

	private response(
		command: RpcCommand,
		success: boolean,
		data?: unknown,
		error?: unknown,
	): RpcResponse {
		return {
			...(command.id === undefined ? {} : { id: command.id }),
			type: "response",
			command: command.type,
			success,
			...(data === undefined ? {} : { data }),
			...(error === undefined
				? {}
				: { error: error instanceof Error ? error.message : String(error) }),
		};
	}
}
