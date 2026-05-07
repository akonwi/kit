import type { AgentRuntimeEvent } from "../runtime/agent-runtime";
import { appendCompaction, appendTurn, type Session } from "../session";
import type { KitAgentMessage, Turn } from "../session/types";

type RuntimeEventSource = {
	subscribe(listener: (event: AgentRuntimeEvent) => void): () => void;
	getSession(): Session;
};

type AutoCompactionPersistence = {
	summaryMessage: Extract<KitAgentMessage, { role: "assistant" }>;
	firstKeptTurnId?: string;
	compactedTurnCount: number;
	keptTurnCount: number;
	tokensBefore: number;
};

export type FilePersistenceFailureEvent = {
	type: "persistence.failed";
	error: string;
};

export class FilePersistence {
	private readonly runtime: RuntimeEventSource;
	private readonly failureListeners = new Set<
		(event: FilePersistenceFailureEvent) => void
	>();
	private unsubscribeRuntime: (() => void) | null = null;
	private writeChain: Promise<void> = Promise.resolve();
	private pendingAutoCompaction: AutoCompactionPersistence | null = null;

	constructor(runtime: RuntimeEventSource) {
		this.runtime = runtime;
		this.unsubscribeRuntime = runtime.subscribe((event) => {
			this.handleRuntimeEvent(event);
		});
	}

	onFailure(
		listener: (event: FilePersistenceFailureEvent) => void,
	): () => void {
		this.failureListeners.add(listener);
		return () => this.failureListeners.delete(listener);
	}

	flush(): Promise<void> {
		return this.writeChain;
	}

	dispose(): void {
		this.unsubscribeRuntime?.();
		this.unsubscribeRuntime = null;
		this.failureListeners.clear();
	}

	private handleRuntimeEvent(event: AgentRuntimeEvent): void {
		switch (event.type) {
			case "session.compaction.completed.auto":
				this.pendingAutoCompaction = {
					summaryMessage: event.summaryMessage,
					firstKeptTurnId: event.firstKeptTurnId,
					compactedTurnCount: event.compactedTurnCount,
					keptTurnCount: event.keptTurnCount,
					tokensBefore: event.tokensBefore,
				};
				break;
			case "agent.turn.completed":
				this.enqueueWrite(() => this.persistCompletedTurn(event.turn));
				break;
		}
	}

	private enqueueWrite(operation: () => Promise<void>): void {
		const next = this.writeChain.then(operation);
		this.writeChain = next.catch(() => {});
		void next.catch((error) => {
			this.emitFailure(error);
		});
	}

	private async persistCompletedTurn(turn: Turn | null): Promise<void> {
		const session = this.runtime.getSession();
		if (turn) {
			await appendTurn(session, turn);
		}
		if (!this.pendingAutoCompaction) return;

		const pending = this.pendingAutoCompaction;
		await appendCompaction({
			session,
			summaryMessage: pending.summaryMessage,
			firstKeptTurnId: pending.firstKeptTurnId,
			compactedTurnCount: pending.compactedTurnCount,
			keptTurnCount: pending.keptTurnCount,
			tokensBefore: pending.tokensBefore,
		});
		if (this.pendingAutoCompaction === pending) {
			this.pendingAutoCompaction = null;
		}
	}

	private emitFailure(error: unknown): void {
		const event: FilePersistenceFailureEvent = {
			type: "persistence.failed",
			error: error instanceof Error ? error.message : String(error),
		};
		for (const listener of this.failureListeners) {
			listener(event);
		}
	}
}
