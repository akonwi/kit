import { randomUUID } from "node:crypto";
import {
	type AfterToolCallContext,
	type AfterToolCallResult,
	type AgentMessage,
	type AgentState,
	type AgentTool,
	type BeforeToolCallContext,
	type BeforeToolCallResult,
	Agent as PiAgent,
	type AgentEvent as PiAgentEvent,
	type AgentOptions as PiAgentOptions,
	type StreamFn,
	type ThinkingLevel,
	type ToolExecutionMode,
} from "@earendil-works/pi-agent-core";
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
} from "@earendil-works/pi-ai";
import {
	type MessagePart,
	messagePartToPromptText,
	type UserMultipartMessage,
} from "../messages/parts";
import type { KitAgentMessage, Session, Turn } from "../session/types";
import { type AnyEvent, EventBus } from "./event-bus";

// Re-export upstream types so the rest of the codebase imports them
// from Kit's own boundary instead of reaching into upstream packages.
export type {
	AgentMessage,
	AgentTool,
	BeforeToolCallContext,
	BeforeToolCallResult,
	CustomAgentMessages,
	ThinkingLevel,
} from "@earendil-works/pi-agent-core";
export type {
	Api,
	AssistantMessage,
	ImageContent,
	Model,
	Static,
	TextContent,
	ToolCall,
	ToolResultMessage,
	TSchema,
	Usage,
	UserMessage,
} from "@earendil-works/pi-ai";
export { Type } from "@earendil-works/pi-ai";

export interface AgentOptions extends PiAgentOptions {
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

export type AgentEventMap = {
	// biome-ignore lint/complexity/noBannedTypes: empty event payload
	"agent.start": {};
	"agent.end": { messages: AgentMessage[] };
	"agent.turn.started": { turn: Turn };
	"agent.turn.ended": {
		turn: Turn | null;
		message: AgentMessage;
		toolResults: ToolResultMessage[];
	};
	"message.start": { message: AgentMessage };
	"message.update": {
		message: AgentMessage;
		assistantMessageEvent: AssistantMessageEvent;
	};
	"agent.message.started": {
		turn: Turn;
		message: Extract<AssistantMessage, { role: "assistant" }>;
	};
	"agent.message.updated": {
		turn: Turn;
		message: Extract<AssistantMessage, { role: "assistant" }>;
	};
	"user.message.created": {
		turn: Turn;
		message: Extract<KitAgentMessage, { role: "user" }>;
	};
	"agent.message.ended": {
		turn: Turn;
		message: Extract<KitAgentMessage, { role: "assistant" }>;
	};
	"message.committed": { turn: Turn; message: KitAgentMessage };
	"agent.thinking.started": { turn: Turn };
	"agent.thinking.updated": { turn: Turn; delta: string };
	"agent.thinking.completed": { turn: Turn };
	"agent.tool.started": {
		turn: Turn;
		toolCallId: string;
		toolName: string;
		args: unknown;
	};
	"agent.tool.updated": {
		turn: Turn;
		toolCallId: string;
		toolName: string;
		args: unknown;
		partialResult: unknown;
	};
	"agent.tool.ended": {
		turn: Turn;
		toolCallId: string;
		toolName: string;
		args: unknown;
		result: unknown;
		isError: boolean;
	};
};

export type AgentEvent = AnyEvent<AgentEventMap>;

export class Agent {
	private readonly pi: PiAgent;
	private readonly bus = new EventBus<AgentEventMap>();
	private readonly unsubscribePi: () => void;
	private readonly toolArgsById = new Map<string, unknown>();
	private disposed = false;
	private _turns: Turn[] = [];
	private _currentTurn: Turn | null = null;
	private _activeFollowUpTurn: Turn | null = null;
	private _pendingFollowUps: string[] = [];
	private _queuedFollowUps: AgentMessage[] = [];
	private nextPromptStartsNewTurn = false;

	static fromSession(session: Session, opts?: AgentOptions): Agent {
		return new Agent({
			...opts,
			initialTurns: session.turns,
		});
	}

	constructor(opts?: AgentOptions) {
		const initialTurns = opts?.initialTurns;
		const initialMessages =
			initialTurns?.flatMap((turn) => turn.messages) ?? [];
		this.pi = new PiAgent({
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

		this.unsubscribePi = this.pi.subscribe((event) => {
			for (const nextEvent of this.processPiEvent(event)) {
				const { type, ...payload } = nextEvent;
				// biome-ignore lint/suspicious/noExplicitAny: event is already a valid union member
				this.bus.publish(type, payload as any);
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
		return this.bus.subscribe(fn);
	}

	setSystemPrompt(v: string): void {
		this.pi.state.systemPrompt = v;
	}

	setModel(model: Model<Api>): void {
		this.pi.state.model = model;
	}

	setThinkingLevel(level: ThinkingLevel): void {
		this.pi.state.thinkingLevel = level;
	}

	setSteeringMode(mode: "all" | "one-at-a-time"): void {
		this.pi.steeringMode = mode;
	}

	getSteeringMode(): "all" | "one-at-a-time" {
		return this.pi.steeringMode;
	}

	setFollowUpMode(mode: "all" | "one-at-a-time"): void {
		this.pi.followUpMode = mode;
	}

	getFollowUpMode(): "all" | "one-at-a-time" {
		return this.pi.followUpMode;
	}

	setTools(tools: AgentTool[]): void {
		this.pi.state.tools = tools;
	}

	replaceMessages(messages: AgentMessage[]): void {
		this.pi.state.messages = messages;
	}

	appendMessage(message: AgentMessage): void {
		this.pi.state.messages = [...this.pi.state.messages, message];
	}

	steer(message: AgentMessage): void {
		this.pi.steer(message);
	}

	followUp(message: AgentMessage): void {
		this.pi.followUp(message);
		this._queuedFollowUps = [...this._queuedFollowUps, message];
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
		this._queuedFollowUps = [];
		this._pendingFollowUps = [];
	}

	clearAllQueues(): void {
		this.pi.clearAllQueues();
		this._queuedFollowUps = [];
		this._pendingFollowUps = [];
	}

	hasQueuedMessages(): boolean {
		return this.pi.hasQueuedMessages();
	}

	clearMessages(): void {
		this.pi.state.messages = [];
	}

	abort(): void {
		this.pi.abort();
	}

	waitForIdle(): Promise<void> {
		return this.pi.waitForIdle();
	}

	reset(): void {
		this.pi.reset();
		this.clearLocalState();
	}

	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		this.abort();
		this.unsubscribePi();
		this.bus.dispose();
		this.clearAllQueues();
		this.clearMessages();
		this.clearLocalState();
	}

	private clearLocalState(): void {
		this.toolArgsById.clear();
		this._turns = [];
		this._currentTurn = null;
		this._activeFollowUpTurn = null;
		this._pendingFollowUps = [];
		this._queuedFollowUps = [];
		this.nextPromptStartsNewTurn = false;
	}

	prompt(message: AgentMessage | AgentMessage[]): Promise<void>;
	prompt(input: string, images?: ImageContent[]): Promise<void>;
	prompt(
		input: AgentMessage | AgentMessage[] | string,
		images?: ImageContent[],
	): Promise<void> {
		if (typeof input === "string") {
			this.nextPromptStartsNewTurn = true;
			const run = this.pi.prompt(input, images);
			void run.catch(() => {
				this.nextPromptStartsNewTurn = false;
			});
			return run;
		}

		this.recordSubmittedUserMessages(input);
		return this.pi.prompt(input);
	}

	private recordSubmittedUserMessages(
		input: AgentMessage | AgentMessage[],
	): void {
		const messages = Array.isArray(input) ? input : [input];
		const userMessages = messages.filter(
			(message): message is Extract<AgentMessage, { role: "user" }> =>
				message.role === "user",
		);
		if (userMessages.length === 0) return;

		let turn = this.startTurn();
		this.bus.publish("agent.turn.started", { turn });
		for (const message of userMessages) {
			const tagged = {
				...message,
				turnId: turn.id,
			} as KitAgentMessage;
			turn = {
				...turn,
				messages: [...turn.messages, tagged],
			};
			this._currentTurn = turn;
			this._turns = this._turns.map((candidate) =>
				candidate.id === turn.id ? turn : candidate,
			);
			this.bus.publish("user.message.created", {
				turn,
				message: tagged as Extract<KitAgentMessage, { role: "user" }>,
			});
			this.bus.publish("message.committed", {
				turn,
				message: tagged,
			});
		}
	}

	continue(): Promise<void> {
		return this.pi.continue();
	}

	setTransport(value: Transport): void {
		this.pi.transport = value;
	}

	setToolExecution(value: ToolExecutionMode): void {
		this.pi.toolExecution = value;
	}

	setBeforeToolCall(
		value:
			| ((
					context: BeforeToolCallContext,
					signal?: AbortSignal,
			  ) => Promise<BeforeToolCallResult | undefined>)
			| undefined,
	): void {
		this.pi.beforeToolCall = value;
	}

	setAfterToolCall(
		value:
			| ((
					context: AfterToolCallContext,
					signal?: AbortSignal,
			  ) => Promise<AfterToolCallResult | undefined>)
			| undefined,
	): void {
		this.pi.afterToolCall = value;
	}

	getPendingFollowUps(): string[] {
		return [...this._pendingFollowUps];
	}

	getPendingFollowUpDrafts(): string[] {
		return this._queuedFollowUps.map((message) => extractEditableText(message));
	}

	setPendingFollowUps(messages: string[]): void {
		this.replacePendingFollowUps(
			messages.flatMap((text) => {
				const trimmed = text.trim();
				return trimmed
					? [
							{
								role: "user" as const,
								content: trimmed,
								timestamp: Date.now(),
							},
						]
					: [];
			}),
		);
	}

	updatePendingFollowUp(index: number, text: string): void {
		if (index < 0 || index >= this._queuedFollowUps.length) return;
		const trimmed = text.trim();
		if (!trimmed) {
			this.removePendingFollowUp(index);
			return;
		}
		this.replacePendingFollowUps(
			this._queuedFollowUps.map((message, messageIndex) =>
				messageIndex === index
					? withPlainTextContent(message, trimmed)
					: message,
			),
		);
	}

	removePendingFollowUp(index: number): void {
		if (index < 0 || index >= this._queuedFollowUps.length) return;
		this.replacePendingFollowUps(
			this._queuedFollowUps.filter((_, messageIndex) => messageIndex !== index),
		);
	}

	drainPendingFollowUps(): string[] {
		const drained = [...this._pendingFollowUps];
		this.clearFollowUpQueue();
		return drained;
	}

	drainPendingFollowUpMessages(): AgentMessage[] {
		const drained = [...this._queuedFollowUps];
		this.clearFollowUpQueue();
		return drained;
	}

	clearPendingFollowUps(): void {
		this.clearFollowUpQueue();
	}

	private replacePendingFollowUps(messages: AgentMessage[]): void {
		this.clearFollowUpQueue();
		for (const message of messages) {
			this.followUp(message);
		}
	}

	private processPiEvent(event: PiAgentEvent): AgentEvent[] {
		switch (event.type) {
			case "turn_start": {
				this._activeFollowUpTurn = null;
				if (this.nextPromptStartsNewTurn || this._currentTurn === null) {
					this.nextPromptStartsNewTurn = false;
					const turn = this.startTurn();
					return [{ type: "agent.turn.started", turn }];
				}
				return [];
			}
			case "message_start": {
				if (event.message.role !== "assistant")
					return [{ type: "message.start", message: event.message }];
				const turn = this.ensureCurrentTurn();
				return [
					{ type: "message.start", message: event.message },
					{
						type: "agent.message.started",
						turn,
						message: event.message as Extract<
							AssistantMessage,
							{ role: "assistant" }
						>,
					},
					{ type: "agent.thinking.started", turn },
				];
			}
			case "message_update": {
				if (event.message.role !== "assistant")
					return [
						{
							type: "message.update",
							message: event.message,
							assistantMessageEvent: event.assistantMessageEvent,
						},
					];
				const turn = this.ensureCurrentTurn();
				const events: AgentEvent[] = [
					{
						type: "message.update",
						message: event.message,
						assistantMessageEvent: event.assistantMessageEvent,
					},
					{
						type: "agent.message.updated",
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
					events.push({ type: "agent.thinking.updated", turn, delta });
				}
				return events;
			}
			case "message_end": {
				const isQueuedFollowUp =
					event.message.role === "user" &&
					this.consumeQueuedFollowUp(event.message);
				const startsFollowUpTurn =
					isQueuedFollowUp && this._activeFollowUpTurn === null;
				const turn = isQueuedFollowUp
					? (this._activeFollowUpTurn ?? this.startTurn())
					: this.ensureCurrentTurn();
				const tagged: KitAgentMessage = {
					...event.message,
					turnId: turn.id,
				};
				const isDuplicateSubmittedUser =
					tagged.role === "user" &&
					turn.messages.some(
						(message) =>
							message.role === "user" && isSameAgentMessage(message, tagged),
					);
				const updatedTurn: Turn = isDuplicateSubmittedUser
					? turn
					: {
							...turn,
							messages: [...turn.messages, tagged],
						};
				this._currentTurn = updatedTurn;
				if (isQueuedFollowUp) this._activeFollowUpTurn = updatedTurn;
				this._turns = this._turns.map((candidate) =>
					candidate.id === updatedTurn.id ? updatedTurn : candidate,
				);
				const events: AgentEvent[] = startsFollowUpTurn
					? [{ type: "agent.turn.started", turn: updatedTurn }]
					: [];
				if (tagged.role === "assistant") {
					events.push({
						type: "agent.thinking.completed",
						turn: updatedTurn,
					});
					events.push({
						type: "agent.message.ended",
						turn: updatedTurn,
						message: tagged as Extract<KitAgentMessage, { role: "assistant" }>,
					});
				}
				if (tagged.role === "user" && !isDuplicateSubmittedUser) {
					events.push({
						type: "user.message.created",
						turn: updatedTurn,
						message: tagged as Extract<KitAgentMessage, { role: "user" }>,
					});
				}
				if (!isDuplicateSubmittedUser) {
					events.push({
						type: "message.committed",
						turn: updatedTurn,
						message: tagged,
					});
				}
				return events;
			}
			case "tool_execution_start": {
				const turn = this.ensureCurrentTurn();
				this.toolArgsById.set(event.toolCallId, event.args);
				return [
					{
						type: "agent.tool.started",
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
						type: "agent.tool.updated",
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
						type: "agent.tool.ended",
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
				this._activeFollowUpTurn = null;
				return [
					{
						type: "agent.turn.ended",
						turn: this._currentTurn,
						message: event.message,
						toolResults: event.toolResults,
					},
				];
			case "agent_start":
				return [{ type: "agent.start" }];
			case "agent_end":
				return [{ type: "agent.end", messages: event.messages }];
			default:
				console.warn(
					`[Agent] unhandled Pi event dropped: ${(event as { type: string }).type}`,
				);
				return [];
		}
	}

	private consumeQueuedFollowUp(message: AgentMessage): boolean {
		const index = this._queuedFollowUps.findIndex((candidate) =>
			isSameAgentMessage(candidate, message),
		);
		if (index < 0) return false;
		this._queuedFollowUps = this._queuedFollowUps.filter(
			(_, candidateIndex) => candidateIndex !== index,
		);
		this._pendingFollowUps = this._pendingFollowUps.filter(
			(_, candidateIndex) => candidateIndex !== index,
		);
		return true;
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
		this.pi.state.messages = [...this.pi.state.messages, message];
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
		this.pi.state.messages = messages;
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
		this._activeFollowUpTurn = null;
		this._pendingFollowUps = [];
		this._queuedFollowUps = [];
		this.nextPromptStartsNewTurn = false;
		this.toolArgsById.clear();
		const messages = this._turns.flatMap(
			(turn) => turn.messages,
		) as AgentMessage[];
		this.pi.state.messages = messages;
	}
}

function isSameAgentMessage(a: AgentMessage, b: AgentMessage): boolean {
	return (
		a.role === b.role &&
		"timestamp" in a &&
		"timestamp" in b &&
		a.timestamp === b.timestamp &&
		"content" in a &&
		"content" in b &&
		JSON.stringify(a.content) === JSON.stringify(b.content)
	);
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
				if (!msg.excludeFromContext) {
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

function withPlainTextContent(
	message: AgentMessage,
	text: string,
): AgentMessage {
	if (!("content" in message)) return message;
	if (typeof message.content === "string") {
		return { ...message, content: text } as AgentMessage;
	}
	if (!Array.isArray(message.content)) return message;
	const nonTextParts = message.content.filter(
		(part) =>
			!(
				typeof part === "object" &&
				part !== null &&
				"type" in part &&
				part.type === "text"
			),
	);
	return {
		...message,
		content: [{ type: "text", text }, ...nonTextParts],
	} as AgentMessage;
}

function extractEditableText(message: AgentMessage): string {
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

function extractPlainText(message: AgentMessage): string {
	if (!("content" in message)) return "";
	const { content } = message;
	if (typeof content === "string") return content;
	if (!Array.isArray(content)) return "";
	return content
		.map((block) => {
			if (typeof block !== "object" || block === null || !("type" in block)) {
				return "";
			}
			return messagePartToPromptText(block as MessagePart);
		})
		.filter((text) => text.trim().length > 0)
		.join("\n");
}
