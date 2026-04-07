import { randomUUID } from "node:crypto";
import {
	Agent,
	type AgentMessage,
	type AgentOptions,
} from "@mariozechner/pi-agent-core";
import type { KitAgentMessage, Session, Turn } from "../session/types";

const DEFAULT_SYSTEM_PROMPT = `You are kit, a coding assistant running in the terminal.
You have access to tools to read and modify files, run commands, search code, and more.
Be concise and direct. Prefer surgical edits over full rewrites when practical.`;

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
				systemPrompt: opts?.initialState?.systemPrompt ?? DEFAULT_SYSTEM_PROMPT,
				thinkingLevel: opts?.initialState?.thinkingLevel ?? "medium",
				...opts?.initialState,
				messages:
					initialMessages.length > 0
						? initialMessages
						: (opts?.initialState?.messages ?? []),
			},
			steeringMode: opts?.steeringMode ?? "all",
			followUpMode: opts?.followUpMode ?? "all",
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
	 * Replace the agent's message history from a persisted turn list.
	 * Restores both the raw messages (for the LLM context) and the turn structure.
	 */
	replaceFromTurns(turns: Turn[]): void {
		this._turns = turns.map((turn) => ({
			...turn,
			messages: [...turn.messages],
		}));
		this._currentTurn = null;
		const messages = this._turns.flatMap(
			(turn) => turn.messages,
		) as AgentMessage[];
		this.replaceMessages(messages);
	}
}
