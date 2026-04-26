import { randomUUID } from "node:crypto";
import "./custom-messages";
import {
	type AfterToolCallContext,
	type AfterToolCallResult,
	Agent,
	type AgentEvent,
	type AgentMessage,
	type AgentOptions,
	type AgentState,
	type AgentTool,
	type BeforeToolCallContext,
	type BeforeToolCallResult,
	type StreamFn,
	type ThinkingLevel,
	type ToolExecutionMode,
} from "@mariozechner/pi-agent-core";
import type {
	Api,
	ImageContent,
	Message,
	Model,
	TextContent,
	ThinkingBudgets,
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

export class KitAgent {
	private readonly pi: Agent;
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
			switch (event.type) {
				case "turn_start": {
					this._pendingFollowUps = [];
					this.startTurn();
					break;
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
					break;
				}
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
		return this.pi.subscribe(fn);
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

	private startTurn(): Turn {
		const turn: Turn = { id: randomUUID(), messages: [] };
		this._turns = [...this._turns, turn];
		this._currentTurn = turn;
		return turn;
	}

	private ensureCurrentTurn(): Turn {
		return this._currentTurn ?? this.startTurn();
	}

	appendCustomMessage(message: AgentMessage): void {
		const turn = this.ensureCurrentTurn();
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
	}

	replaceCustomMessage(
		predicate: (message: AgentMessage) => boolean,
		next: AgentMessage,
	): void {
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
		if (!replaced) return;
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
	}

	replaceFromTurns(turns: Turn[]): void {
		this._turns = turns.map((turn) => ({
			...turn,
			messages: [...turn.messages],
		}));
		this._currentTurn = null;
		this._pendingFollowUps = [];
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
