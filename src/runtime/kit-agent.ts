import { randomUUID } from "node:crypto";
import "./custom-messages";
import {
	Agent,
	type AgentMessage,
	type AgentOptions,
} from "@mariozechner/pi-agent-core";
import type { Message, UserMessage } from "@mariozechner/pi-ai";
import type { KitAgentMessage, Session, Turn } from "../session/types";

export interface KitAgentOptions extends AgentOptions {
	initialTurns?: Turn[];
}

/**
 * Extends pi-agent-core's Agent with explicit turn tracking.
 *
 * Every message is tagged with a turnId at the moment it arrives,
 * so grouping is always O(1) rather than derived by scanning.
 */
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
			convertToLlm: convertToLlm,
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

	/**
	 * Append a custom message (e.g. bashExecution) to the current turn.
	 * Creates a new turn if none is active. Also appends to the Agent's
	 * message list so it's included in future context.
	 */
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

	/**
	 * Replace the agent's message history from a persisted turn list.
	 * Restores both the raw messages (for the LLM context) and the turn structure.
	 */
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

/**
 * Convert AgentMessage[] to LLM-compatible Message[].
 * Filters out custom message types that the LLM doesn't understand,
 * optionally converting some to user messages for context.
 */
function convertToLlm(messages: AgentMessage[]): Message[] {
	const result: Message[] = [];
	for (const msg of messages) {
		switch (msg.role) {
			case "user":
			case "assistant":
			case "toolResult":
				result.push(msg as Message);
				break;
			case "bashExecution": {
				// Include in LLM context as a user message unless excluded
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
			// Unknown custom roles are silently dropped
		}
	}
	return result;
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
