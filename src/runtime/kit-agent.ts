import { randomUUID } from "node:crypto";
import "./custom-messages";
import {
	type AfterToolCallContext,
	type AfterToolCallResult,
	Agent,
	type AgentMessage,
	type AgentOptions,
	type AgentState,
	type AgentTool,
	type BeforeToolCallContext,
	type BeforeToolCallResult,
	type AgentEvent as PiAgentEvent,
	type StreamFn,
	type ThinkingLevel,
	type ToolExecutionMode,
} from "@mariozechner/pi-agent-core";
import type {
	Api,
	AssistantMessage,
	AssistantMessageEvent,
	ImageContent,
	Message,
	Model,
	TextContent,
	ThinkingBudgets,
	ToolResultMessage,
	Transport,
	UserMessage,
} from "@mariozechner/pi-ai";
import {
	type MessagePart,
	messagePartToPromptText,
	type UserMultipartMessage,
} from "../messages/parts";
import type { KitAgentMessage, Session, Turn } from "../session/types";

export interface KitAgentOptions extends AgentOptions {
	initialTurns?: Turn[];
}

export type AppendedCustomMessage = {
	turn: Turn;
	message: KitAgentMessage;
	createdTurn: boolean;
};

export type ReplacedCustomMessage = {
	turn: Turn;
	message: KitAgentMessage;
};

export type AgentEvent =
	| { type: "agent_start" }
	| { type: "agent_end"; messages: AgentMessage[] }
	| { type: "turn_start"; turn: Turn }
	| {
			type: "turn_end";
			turn: Turn | null;
			message: AgentMessage;
			toolResults: ToolResultMessage[];
	  }
	| { type: "message_start"; message: AgentMessage }
	| {
			type: "message_update";
			message: AgentMessage;
			assistantMessageEvent: AssistantMessageEvent;
	  }
	| {
			type: "assistant_message_started";
			turn: Turn;
			message: Extract<AssistantMessage, { role: "assistant" }>;
	  }
	| {
			type: "assistant_message_updated";
			turn: Turn;
			message: Extract<AssistantMessage, { role: "assistant" }>;
	  }
	| {
			type: "user_message_created";
			turn: Turn;
			message: Extract<KitAgentMessage, { role: "user" }>;
	  }
	| {
			type: "assistant_message_ended";
			turn: Turn;
			message: Extract<KitAgentMessage, { role: "assistant" }>;
	  }
	| { type: "message_end"; turn: Turn; message: KitAgentMessage }
	| { type: "agent_thinking_started"; turn: Turn }
	| { type: "agent_thinking_updated"; turn: Turn; delta: string }
	| { type: "agent_thinking_completed"; turn: Turn }
	| {
			type: "agent_tool_started";
			turn: Turn;
			toolCallId: string;
			toolName: string;
			args: unknown;
	  }
	| {
			type: "agent_tool_updated";
			turn: Turn;
			toolCallId: string;
			toolName: string;
			args: unknown;
			partialResult: unknown;
	  }
	| {
			type: "agent_tool_ended";
			turn: Turn;
			toolCallId: string;
			toolName: string;
			args: unknown;
			result: unknown;
			isError: boolean;
	  };

export class KitAgent {
	private readonly pi: Agent;
	private readonly listeners = new Set<(event: AgentEvent) => void>();
	private readonly toolArgsById = new Map<string, unknown>();
	private _turns: Turn[] = [];
	private _currentTurn: Turn | null = null;
	private _pendingFollowUps: string[] = [];

	static fromSession(session: Session, opts?: KitAgentOptions): KitAgent {
		return new KitAgent({
			...opts,
			initialTurns: session.turns,
		});
	}

	constructor(opts?: KitAgentOptions) {
		const initialTurns = opts?.initialTurns;
		const initialMessages =
			initialTurns?.flatMap((turn) => turn.messages) ?? [];
		this.pi = new Agent({
			...opts,
			initialState: {
				systemPrompt: opts?.initialState?.systemPrompt ?? "",
				thinkingLevel: opts?.initialState?.thinkingLevel ?? "medium",
				...opts?.initialState,
				messages:
					initialMessages.length > 0
						? initialMessages
						: (opts?.initialState?.messages ?? []),
			},
			steeringMode: opts?.steeringMode ?? "all",
			followUpMode: opts?.followUpMode ?? "all",
			convertToLlm,
		});

		if (initialTurns) {
			this._turns = initialTurns.map((turn) => ({
				...turn,
				messages: [...turn.messages],
			}));
		}

		this.pi.subscribe((event) => {
			for (const nextEvent of this.processPiEvent(event)) {
				this.emit(nextEvent);
			}
		});
	}

	get sessionId(): string | undefined {
		return this.pi.sessionId;
	}

	set sessionId(value: string | undefined) {
		this.pi.sessionId = value;
	}

	get thinkingBudgets(): ThinkingBudgets | undefined {
		return this.pi.thinkingBudgets;
	}

	set thinkingBudgets(value: ThinkingBudgets | undefined) {
		this.pi.thinkingBudgets = value;
	}

	get transport(): Transport {
		return this.pi.transport;
	}

	get maxRetryDelayMs(): number | undefined {
		return this.pi.maxRetryDelayMs;
	}

	set maxRetryDelayMs(value: number | undefined) {
		this.pi.maxRetryDelayMs = value;
	}

	get toolExecution(): ToolExecutionMode {
		return this.pi.toolExecution;
	}

	get state(): AgentState {
		return this.pi.state;
	}

	get streamFn(): StreamFn {
		return this.pi.streamFn;
	}

	set streamFn(value: StreamFn) {
		this.pi.streamFn = value;
	}

	get getApiKey():
		| ((provider: string) => Promise<string | undefined> | string | undefined)
		| undefined {
		return this.pi.getApiKey;
	}

	set getApiKey(value:
		| ((provider: string) => Promise<string | undefined> | string | undefined)
		| undefined,) {
		this.pi.getApiKey = value;
	}

	get turns(): Turn[] {
		return this._turns;
	}

	subscribe(fn: (e: AgentEvent) => void): () => void {
		this.listeners.add(fn);
		return () => this.listeners.delete(fn);
	}

	setSystemPrompt(v: string): void {
		this.pi.setSystemPrompt(v);
	}

	setModel(model: Model<Api>): void {
		this.pi.setModel(model);
	}

	setThinkingLevel(level: ThinkingLevel): void {
		this.pi.setThinkingLevel(level);
	}

	setSteeringMode(mode: "all" | "one-at-a-time"): void {
		this.pi.setSteeringMode(mode);
	}

	getSteeringMode(): "all" | "one-at-a-time" {
		return this.pi.getSteeringMode();
	}

	setFollowUpMode(mode: "all" | "one-at-a-time"): void {
		this.pi.setFollowUpMode(mode);
	}

	getFollowUpMode(): "all" | "one-at-a-time" {
		return this.pi.getFollowUpMode();
	}

	setTools(tools: AgentTool[]): void {
		this.pi.setTools(tools);
	}

	replaceMessages(messages: AgentMessage[]): void {
		this.pi.replaceMessages(messages);
	}

	appendMessage(message: AgentMessage): void {
		this.pi.appendMessage(message);
	}

	steer(message: AgentMessage): void {
		this.pi.steer(message);
	}

	followUp(message: AgentMessage): void {
		this.pi.followUp(message);
		const text = extractPlainText(message);
		if (text.trim()) {
			this._pendingFollowUps = [...this._pendingFollowUps, text];
		}
	}

	clearSteeringQueue(): void {
		this.pi.clearSteeringQueue();
	}

	clearFollowUpQueue(): void {
		this.pi.clearFollowUpQueue();
	}

	clearAllQueues(): void {
		this.pi.clearAllQueues();
	}

	hasQueuedMessages(): boolean {
		return this.pi.hasQueuedMessages();
	}

	clearMessages(): void {
		this.pi.clearMessages();
	}

	abort(): void {
		this.pi.abort();
	}

	waitForIdle(): Promise<void> {
		return this.pi.waitForIdle();
	}

	reset(): void {
		this.pi.reset();
		this.toolArgsById.clear();
		this._turns = [];
		this._currentTurn = null;
		this._pendingFollowUps = [];
	}

	prompt(message: AgentMessage | AgentMessage[]): Promise<void>;
	prompt(input: string, images?: ImageContent[]): Promise<void>;
	prompt(
		input: AgentMessage | AgentMessage[] | string,
		images?: ImageContent[],
	): Promise<void> {
		if (typeof input === "string") {
			return this.pi.prompt(input, images);
		}
		return this.pi.prompt(input);
	}

	continue(): Promise<void> {
		return this.pi.continue();
	}

	setTransport(value: Transport): void {
		this.pi.setTransport(value);
	}

	setToolExecution(value: ToolExecutionMode): void {
		this.pi.setToolExecution(value);
	}

	setBeforeToolCall(
		value:
			| ((
					context: BeforeToolCallContext,
					signal?: AbortSignal,
			  ) => Promise<BeforeToolCallResult | undefined>)
			| undefined,
	): void {
		this.pi.setBeforeToolCall(value);
	}

	setAfterToolCall(
		value:
			| ((
					context: AfterToolCallContext,
					signal?: AbortSignal,
			  ) => Promise<AfterToolCallResult | undefined>)
			| undefined,
	): void {
		this.pi.setAfterToolCall(value);
	}

	getPendingFollowUps(): string[] {
		return [...this._pendingFollowUps];
	}

	drainPendingFollowUps(): string[] {
		const drained = [...this._pendingFollowUps];
		this.clearAllQueues();
		this._pendingFollowUps = [];
		return drained;
	}

	clearPendingFollowUps(): void {
		this.clearAllQueues();
		this._pendingFollowUps = [];
	}

	private emit(event: AgentEvent): void {
		for (const listener of this.listeners) {
			listener(event);
		}
	}

	private processPiEvent(event: PiAgentEvent): AgentEvent[] {
		switch (event.type) {
			case "turn_start": {
				this._pendingFollowUps = [];
				const turn = this.startTurn();
				return [{ type: "turn_start", turn }];
			}
			case "message_start": {
				if (event.message.role !== "assistant") return [event];
				const turn = this.ensureCurrentTurn();
				return [
					event,
					{
						type: "assistant_message_started",
						turn,
						message: event.message as Extract<
							AssistantMessage,
							{ role: "assistant" }
						>,
					},
					{ type: "agent_thinking_started", turn },
				];
			}
			case "message_update": {
				if (event.message.role !== "assistant") return [event];
				const turn = this.ensureCurrentTurn();
				const events: AgentEvent[] = [
					event,
					{
						type: "assistant_message_updated",
						turn,
						message: event.message as Extract<
							AssistantMessage,
							{ role: "assistant" }
						>,
					},
				];
				const delta =
					event.assistantMessageEvent?.type === "thinking_delta" &&
					"delta" in event.assistantMessageEvent &&
					typeof event.assistantMessageEvent.delta === "string"
						? event.assistantMessageEvent.delta
						: null;
				if (delta) {
					events.push({ type: "agent_thinking_updated", turn, delta });
				}
				return events;
			}
			case "message_end": {
				const turn = this.ensureCurrentTurn();
				const tagged: KitAgentMessage = {
					...event.message,
					turnId: turn.id,
				};
				const updatedTurn: Turn = {
					...turn,
					messages: [...turn.messages, tagged],
				};
				this._currentTurn = updatedTurn;
				this._turns = this._turns.map((candidate) =>
					candidate.id === updatedTurn.id ? updatedTurn : candidate,
				);
				const events: AgentEvent[] = [];
				if (tagged.role === "assistant") {
					events.push({
						type: "agent_thinking_completed",
						turn: updatedTurn,
					});
					events.push({
						type: "assistant_message_ended",
						turn: updatedTurn,
						message: tagged as Extract<KitAgentMessage, { role: "assistant" }>,
					});
				}
				if (tagged.role === "user") {
					events.push({
						type: "user_message_created",
						turn: updatedTurn,
						message: tagged as Extract<KitAgentMessage, { role: "user" }>,
					});
				}
				events.push({
					type: "message_end",
					turn: updatedTurn,
					message: tagged,
				});
				return events;
			}
			case "tool_execution_start": {
				const turn = this.ensureCurrentTurn();
				this.toolArgsById.set(event.toolCallId, event.args);
				return [
					{
						type: "agent_tool_started",
						turn,
						toolCallId: event.toolCallId,
						toolName: event.toolName,
						args: event.args,
					},
				];
			}
			case "tool_execution_update": {
				const turn = this.ensureCurrentTurn();
				this.toolArgsById.set(event.toolCallId, event.args);
				return [
					{
						type: "agent_tool_updated",
						turn,
						toolCallId: event.toolCallId,
						toolName: event.toolName,
						args: event.args,
						partialResult: event.partialResult,
					},
				];
			}
			case "tool_execution_end": {
				const turn = this.ensureCurrentTurn();
				const args = this.toolArgsById.get(event.toolCallId);
				this.toolArgsById.delete(event.toolCallId);
				return [
					{
						type: "agent_tool_ended",
						turn,
						toolCallId: event.toolCallId,
						toolName: event.toolName,
						args,
						result: event.result,
						isError: event.isError,
					},
				];
			}
			case "turn_end":
				return [
					{
						type: "turn_end",
						turn: this._currentTurn,
						message: event.message,
						toolResults: event.toolResults,
					},
				];
			default:
				return [event];
		}
	}

	private startTurn(): Turn {
		const turn: Turn = { id: randomUUID(), messages: [] };
		this._turns = [...this._turns, turn];
		this._currentTurn = turn;
		return turn;
	}

	private ensureCurrentTurn(): Turn {
		return this._currentTurn ?? this.startTurn();
	}

	private ensureCurrentTurnWithCreation(): {
		turn: Turn;
		createdTurn: boolean;
	} {
		if (this._currentTurn) {
			return { turn: this._currentTurn, createdTurn: false };
		}
		return { turn: this.startTurn(), createdTurn: true };
	}

	appendCustomMessage(message: AgentMessage): AppendedCustomMessage {
		const { turn, createdTurn } = this.ensureCurrentTurnWithCreation();
		const tagged: KitAgentMessage = {
			...message,
			turnId: turn.id,
		};
		const updatedTurn: Turn = {
			...turn,
			messages: [...turn.messages, tagged],
		};
		this._currentTurn = updatedTurn;
		this._turns = this._turns.map((candidate) =>
			candidate.id === updatedTurn.id ? updatedTurn : candidate,
		);
		this.pi.appendMessage(message);
		return {
			turn: updatedTurn,
			message: tagged,
			createdTurn,
		};
	}

	replaceCustomMessage(
		predicate: (message: AgentMessage) => boolean,
		next: AgentMessage,
	): ReplacedCustomMessage | null {
		let replaced = false;
		this._turns = this._turns.map((turn) => ({
			...turn,
			messages: turn.messages.map((message) => {
				if (!predicate(message)) return message;
				replaced = true;
				return {
					...next,
					turnId: message.turnId,
				} as KitAgentMessage;
			}),
		}));
		if (!replaced) return null;
		if (this._currentTurn) {
			const nextTurn = this._turns.find(
				(turn) => turn.id === this._currentTurn?.id,
			);
			if (nextTurn) this._currentTurn = nextTurn;
		}
		const messages = this._turns.flatMap(
			(turn) => turn.messages,
		) as AgentMessage[];
		this.pi.replaceMessages(messages);
		for (const turn of this._turns) {
			const message = turn.messages.find((candidate) => predicate(candidate));
			if (message) {
				return {
					turn,
					message,
				};
			}
		}
		return null;
	}

	replaceFromTurns(turns: Turn[]): void {
		this._turns = turns.map((turn) => ({
			...turn,
			messages: [...turn.messages],
		}));
		this._currentTurn = null;
		this._pendingFollowUps = [];
		this.toolArgsById.clear();
		const messages = this._turns.flatMap(
			(turn) => turn.messages,
		) as AgentMessage[];
		this.pi.replaceMessages(messages);
	}
}

function convertToLlm(messages: AgentMessage[]): Message[] {
	const result: Message[] = [];
	for (const msg of messages) {
		switch (msg.role) {
			case "user": {
				const normalized = normalizeUserMessage(
					msg as UserMessage | UserMultipartMessage,
				);
				result.push(normalized);
				break;
			}
			case "assistant":
			case "toolResult":
				result.push(msg as Message);
				break;
			case "bashExecution": {
				if (!msg.pending && !msg.excludeFromContext) {
					const exitInfo =
						msg.exitCode != null && msg.exitCode !== 0
							? ` (exit code: ${msg.exitCode})`
							: "";
					const userMsg: UserMessage = {
						role: "user",
						content: `[bash command: ${msg.command}]${exitInfo}\n${msg.output}`,
						timestamp: msg.timestamp,
					};
					result.push(userMsg);
				}
				break;
			}
		}
	}
	return result;
}

function normalizeUserMessage(
	message: UserMessage | UserMultipartMessage,
): UserMessage {
	if (typeof message.content === "string") {
		return {
			role: "user",
			content: message.content,
			timestamp: message.timestamp,
		};
	}
	const content: Array<TextContent | ImageContent> = [];
	for (const part of message.content as Array<
		TextContent | ImageContent | MessagePart
	>) {
		if (part.type === "image" && "data" in part && "mimeType" in part) {
			content.push(part as ImageContent);
			continue;
		}
		const promptText = messagePartToPromptText(part as MessagePart);
		if (promptText) {
			content.push({ type: "text", text: promptText });
		}
	}
	return {
		role: "user",
		content,
		timestamp: message.timestamp,
	};
}

function extractPlainText(message: AgentMessage): string {
	if (!("content" in message)) return "";
	const { content } = message;
	if (typeof content === "string") return content;
	if (!Array.isArray(content)) return "";
	return content
		.filter(
			(
				block,
			): block is {
				type: "text";
				text: string;
			} =>
				typeof block === "object" &&
				block !== null &&
				"type" in block &&
				block.type === "text" &&
				"text" in block &&
				typeof block.text === "string",
		)
		.map((block) => block.text)
		.join("\n");
}
