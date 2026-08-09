import type { CommandRegistry } from "../features/commands";
import type { MessagePart } from "../messages/parts";
import type { ThinkingLevel } from "../runtime/agent";
import type { AgentRuntime, AgentRuntimeEvent } from "../runtime/agent-runtime";
import { getAvailableThinkingLevels } from "../runtime/thinking-levels";
import { writeSession } from "../session";
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
	messageOffset: number;
	totalMessageCount: number;
	pendingInteractions: unknown[];
	pendingInteractionGeneration: number;
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
] as const;

export type RpcSessionHostOptions = {
	persistSessions?: boolean;
	interactions?: RemoteInteractionBroker;
	attachments?: RemoteAttachmentStore;
	commands?: CommandRegistry;
	waitForWorkspaceReady?: () => Promise<void>;
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
				},
			];
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
		case "session.compaction.completed.recovery":
		case "session.compaction.completed.adaptation":
		case "session.compaction.completed.manual":
			return [{ type: "session.transcript.replaced", reason: "compaction" }];
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
	private readonly unsubscribeRuntime: () => void;
	private readonly unsubscribeInteractions: (() => void) | null;
	private readonly unsubscribeCommands: (() => void) | null;
	private readonly persistSessions: boolean;
	private readonly interactions?: RemoteInteractionBroker;
	private readonly attachments?: RemoteAttachmentStore;
	private readonly commands?: CommandRegistry;
	private readonly waitForWorkspaceReady: () => Promise<void>;
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
		this.commands = options.commands;
		this.waitForWorkspaceReady =
			options.waitForWorkspaceReady ?? (async () => {});
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
				event.type === "session.active.changed" ||
				event.type === "session.model.changed" ||
				event.type === "session.thinking_level.changed"
			) {
				this.publishStateChanged();
			}
		});
		this.unsubscribeInteractions =
			this.interactions?.subscribe((event) => this.publish(event)) ?? null;
		this.unsubscribeCommands =
			this.commands?.subscribe(() => {
				this.commandRegistryGeneration += 1;
			}) ?? null;
	}

	subscribe(listener: RpcEventListener): () => void {
		if (this.disposed) return () => {};
		this.listeners.add(listener);
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
		this.unsubscribeCommands?.();
		this.interactions?.dispose();
		this.attachments?.dispose();
		this.listeners.clear();
	}

	private publish(record: unknown): void {
		for (const listener of [...this.listeners]) {
			try {
				listener(record);
			} catch (error) {
				console.error(
					`RPC event listener failed: ${error instanceof Error ? error.message : String(error)}`,
				);
			}
		}
	}

	private stateSnapshot(): Record<string, unknown> {
		const session = this.runtime.getSession();
		return {
			model: this.runtime.getCurrentModel(),
			thinkingLevel: this.runtime.agentInfo.thinkingLevel,
			isStreaming: this.runtime.getStatus().isStreaming,
			sessionId: session.id,
			sessionName: session.name,
			cwd: session.cwd,
			messageCount: this.runtime.getMessages().length,
			pendingMessageCount: this.runtime.getPendingMessageCount(),
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
		handler: (args: string, signal?: AbortSignal) => void | Promise<void>,
		args: string,
	): Promise<void> {
		const abortController = new AbortController();
		const timeout = setTimeout(() => {
			abortController.abort(new Error("Command execution timed out"));
		}, this.commandTimeoutMs);
		const handlerExecution = Promise.resolve().then(() =>
			handler(args, abortController.signal),
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
		} catch (error) {
			if (
				abortController.signal.aborted &&
				!(await this.waitForCommandHandlerToStop(handlerExecution))
			) {
				this.commandExecutionCompromised = true;
			}
			throw error;
		} finally {
			clearTimeout(timeout);
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
				try {
					await respond(this.response(command, true));
				} catch (error) {
					this.promptReserved = false;
					claim?.release();
					throw error;
				}
				if (!this.acceptingCommands) {
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
				await this.runtime.newSession();
				await this.runtime.waitForModelAdaptation();
				await this.waitForWorkspaceReady();
				if (this.persistSessions) await writeSession(this.runtime.getSession());
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
			case "list_commands": {
				if (!this.commands) throw new Error("Command registry is unavailable");
				const commands = this.commands
					.getAll()
					.filter((candidate) => candidate.executeTransportNeutral)
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
				const registered = this.commands
					.getAll()
					.find((candidate) => candidate.name === commandId);
				if (!registered) throw new Error(`Command not found: ${commandId}`);
				if (!registered.executeTransportNeutral) {
					throw new Error(`Command is not available remotely: ${commandId}`);
				}
				await this.executeTransportNeutralCommand(
					registered.executeTransportNeutral,
					args ?? "",
				);
				await this.waitForWorkspaceReady();
				await respond(this.response(command, true));
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
						],
						interactiveUI: this.interactions !== undefined,
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
