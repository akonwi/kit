import { randomUUID } from "node:crypto";
import "./custom-messages";
import {
	Agent,
	type AgentMessage,
	type AgentOptions,
} from "@mariozechner/pi-agent-core";
import type {
	ImageContent,
	Message,
	TextContent,
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

export class KitAgent extends Agent {
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
		super({
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

		this.subscribe((event) => {
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

	get turns(): Turn[] {
		return this._turns;
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

	override followUp(message: AgentMessage): void {
		super.followUp(message);
		const text = extractPlainText(message);
		if (text.trim()) {
			this._pendingFollowUps = [...this._pendingFollowUps, text];
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
		this.appendMessage(message);
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
		this.replaceMessages(messages);
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
		this.replaceMessages(messages);
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
