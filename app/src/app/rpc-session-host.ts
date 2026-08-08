import type { CommandRegistry } from "../features/commands";
import type { ThinkingLevel } from "../runtime/agent";
import type { AgentRuntime, AgentRuntimeEvent } from "../runtime/agent-runtime";
import { getAvailableThinkingLevels } from "../runtime/thinking-levels";
import { writeSession } from "../session";
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

export const RPC_PROTOCOL_VERSION = 1;

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
	commands?: CommandRegistry;
	waitForWorkspaceReady?: () => Promise<void>;
	allowLegacySessionPaths?: boolean;
	commandTimeoutMs?: number;
	commandCancellationGraceMs?: number;
};

const DEFAULT_COMMAND_TIMEOUT_MS = 30_000;
const DEFAULT_COMMAND_CANCELLATION_GRACE_MS = 2_000;

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
			return [{ type: "agent_start" }];
		case "turn.start":
			return [{ type: "turn_start" }];
		case "message.start":
			return [{ type: "message_start", message: event.message }];
		case "message.update":
			return [
				{
					type: "message_update",
					assistantMessageEvent: event.assistantMessageEvent,
				},
			];
		case "message.end":
			return [{ type: "message_end", message: event.message }];
		case "agent.tool.started":
			return [
				{
					type: "tool_execution_start",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
				},
			];
		case "agent.tool.updated":
			return [
				{
					type: "tool_execution_update",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					partialResult: event.partialResult,
				},
			];
		case "agent.tool.ended":
			return [
				{
					type: "tool_execution_end",
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					result: event.result,
					isError: event.isError,
				},
			];
		case "agent.turn.ended":
			return [
				{
					type: "turn_end",
					message: event.message,
					toolResults: event.toolResults,
				},
			];
		case "agent.end":
			return [
				{
					type: "agent_end",
					messages: event.messages,
					willRetry: event.willRetry ?? false,
				},
			];
		case "agent.settled":
			return [{ type: "agent_settled" }];
		case "chat.message-queue.changed":
			return [
				{
					type: "queue_update",
					steering: event.steering,
					followUp: event.messages,
				},
			];
		case "agent.retry.started":
			return [
				{
					type: "auto_retry_start",
					attempt: event.attempt,
					maxAttempts: event.maxAttempts,
					delayMs: event.delayMs,
				},
			];
		case "agent.retry.failed":
			return [
				{
					type: "auto_retry_end",
					success: false,
					attempt: event.attempt,
					finalError: event.error,
				},
			];
		case "agent.retry.completed":
			return [
				{
					type: "auto_retry_end",
					success: true,
					attempt: event.attempt,
				},
			];
		case "agent.run.failed":
			return [{ type: "error", error: event.error }];
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
	private readonly persistSessions: boolean;
	private readonly interactions?: RemoteInteractionBroker;
	private readonly commands?: CommandRegistry;
	private readonly waitForWorkspaceReady: () => Promise<void>;
	private readonly allowLegacySessionPaths: boolean;
	private readonly commandTimeoutMs: number;
	private readonly commandCancellationGraceMs: number;
	private activeCommandAbort: AbortController | null = null;
	private activeCommandExecution: Promise<void> | null = null;
	private commandGeneration = 0;
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
		this.interactions?.dispose();
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
				const message = requireString(command, "message");
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
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
				this.promptReserved = true;
				await respond(this.response(command, true));
				if (!this.acceptingCommands) {
					this.promptReserved = false;
					return;
				}
				let run: Promise<void>;
				run = Promise.resolve()
					.then(() => this.runtime.submitUserMessage(message))
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
				await respond(this.response(command, true, { commands }));
				return;
			}
			case "execute_command": {
				if (this.promptReserved || this.runtime.getStatus().isStreaming) {
					throw new Error(
						"Cannot execute commands while the agent is streaming",
					);
				}
				if (!this.commands) throw new Error("Command registry is unavailable");
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
							...(this.interactions ? ["ui_response"] : []),
							...(this.commands ? ["list_commands", "execute_command"] : []),
						],
						interactiveUI: this.interactions !== undefined,
						interactionKinds:
							this.interactions === undefined ? [] : REMOTE_INTERACTION_KINDS,
					}),
				);
				return;
			case "get_state":
				await respond(this.response(command, true, this.stateSnapshot()));
				return;
			case "get_messages":
				await respond(
					this.response(command, true, {
						messages: this.runtime.getMessages(),
					}),
				);
				return;
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
