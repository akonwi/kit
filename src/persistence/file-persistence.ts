import type { AgentRuntimeEvent } from "../runtime/agent-runtime";
import {
	appendCompaction,
	appendHandoffSummary,
	appendMessage,
	appendModelChange,
	appendSessionInfo,
	appendThinkingLevelChange,
	appendTurn,
	type Session,
} from "../session";
import type { KitAgentMessage, Turn } from "../session/types";

type RuntimeEventSource = {
	subscribe(listener: (event: AgentRuntimeEvent) => void): () => void;
	getSession(): Session;
};

type CompactionPersistence = {
	summaryMessage: Extract<KitAgentMessage, { role: "assistant" }>;
	firstKeptTurnId?: string;
	compactedTurnCount: number;
	keptTurnCount: number;
	tokensBefore: number;
	keptTurns: Turn[];
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
			case "session.message.appended":
				this.enqueueWrite(() =>
					appendMessage(event.session, event.turn.id, event.message),
				);
				break;
			case "session.compaction.completed.auto": {
				const session = this.runtime.getSession();
				this.enqueueWrite(() => this.persistCompaction(session, event));
				break;
			}
			case "session.compaction.completed.recovery": {
				const session = this.runtime.getSession();
				this.enqueueWrite(() => this.persistCompaction(session, event));
				break;
			}
			case "session.compaction.completed.adaptation": {
				const session = this.runtime.getSession();
				this.enqueueWrite(() => this.persistCompaction(session, event));
				break;
			}
			case "session.handoff_summary.appended":
				this.enqueueWrite(() =>
					appendHandoffSummary(event.session, event.summaryMessage),
				);
				break;
			case "session.name.changed":
				this.enqueueWrite(() => appendSessionInfo(event.session, event.name));
				break;
			case "session.model.changed":
				this.enqueueWrite(() => appendModelChange(event.session));
				break;
			case "session.thinking_level.changed":
				this.enqueueWrite(() => appendThinkingLevelChange(event.session));
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

	private async persistCompaction(
		session: Session,
		compaction: CompactionPersistence,
	): Promise<void> {
		for (const turn of compaction.keptTurns) {
			await appendTurn(session, turn);
		}
		await appendCompaction({
			session,
			summaryMessage: compaction.summaryMessage,
			firstKeptTurnId: compaction.firstKeptTurnId,
			compactedTurnCount: compaction.compactedTurnCount,
			keptTurnCount: compaction.keptTurnCount,
			tokensBefore: compaction.tokensBefore,
		});
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
