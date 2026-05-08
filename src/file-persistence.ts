import type { AgentRuntimeEvent } from "./runtime/agent-runtime";
import {
	appendCompaction,
	appendHandoffSummary,
	appendMessage,
	appendModelChange,
	appendSessionInfo,
	appendThinkingLevelChange,
	appendTurn,
	type Session,
} from "./session";
import type { KitAgentMessage, Turn } from "./session/types";

type RuntimeEventSource = {
	subscribe(listener: (event: AgentRuntimeEvent) => void): () => void;
	getSession(): Session;
};

type FilePersistenceStorage = {
	appendCompaction: typeof appendCompaction;
	appendHandoffSummary: typeof appendHandoffSummary;
	appendMessage: typeof appendMessage;
	appendModelChange: typeof appendModelChange;
	appendSessionInfo: typeof appendSessionInfo;
	appendThinkingLevelChange: typeof appendThinkingLevelChange;
	appendTurn: typeof appendTurn;
};

const defaultStorage: FilePersistenceStorage = {
	appendCompaction,
	appendHandoffSummary,
	appendMessage,
	appendModelChange,
	appendSessionInfo,
	appendThinkingLevelChange,
	appendTurn,
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
	private readonly storage: FilePersistenceStorage;
	private readonly failureListeners = new Set<
		(event: FilePersistenceFailureEvent) => void
	>();
	private unsubscribeRuntime: (() => void) | null = null;
	private queue: Array<() => Promise<void>> = [];
	private flushInFlight: Promise<void> | null = null;

	constructor(
		runtime: RuntimeEventSource,
		storage: FilePersistenceStorage = defaultStorage,
	) {
		this.runtime = runtime;
		this.storage = storage;
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
		return this.flushQueue();
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
					this.storage.appendMessage(
						event.session,
						event.turn.id,
						event.message,
					),
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
					this.storage.appendHandoffSummary(
						event.session,
						event.summaryMessage,
					),
				);
				break;
			case "session.name.changed":
				this.enqueueWrite(() =>
					this.storage.appendSessionInfo(event.session, event.name),
				);
				break;
			case "session.model.changed":
				this.enqueueWrite(() => this.storage.appendModelChange(event.session));
				break;
			case "session.thinking_level.changed":
				this.enqueueWrite(() =>
					this.storage.appendThinkingLevelChange(event.session),
				);
				break;
		}
	}

	private enqueueWrite(operation: () => Promise<void>): void {
		this.queue.push(operation);
		void this.flushQueue();
	}

	private flushQueue(): Promise<void> {
		if (this.flushInFlight) return this.flushInFlight;
		let stoppedAfterFailure = false;
		this.flushInFlight = (async () => {
			try {
				while (this.queue.length > 0) {
					const operation = this.queue[0];
					if (!operation) break;
					try {
						await operation();
						this.queue.shift();
					} catch (error) {
						stoppedAfterFailure = true;
						this.emitFailure(error);
						return;
					}
				}
			} finally {
				this.flushInFlight = null;
				if (this.queue.length > 0 && !stoppedAfterFailure) {
					void this.flushQueue();
				}
			}
		})();
		return this.flushInFlight;
	}

	private async persistCompaction(
		session: Session,
		compaction: CompactionPersistence,
	): Promise<void> {
		for (const turn of compaction.keptTurns) {
			await this.storage.appendTurn(session, turn);
		}
		await this.storage.appendCompaction({
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
