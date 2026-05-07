import { randomUUID } from "node:crypto";
import "./custom-messages";
import type {
	AgentMessage,
	AgentTool,
	ThinkingLevel,
} from "@mariozechner/pi-agent-core";
import {
	type Api,
	getModels,
	type Model,
	registerBuiltInApiProviders,
	type UserMessage,
} from "@mariozechner/pi-ai";
import { getApiKey, getAuthenticatedProviderIds } from "../auth";
import {
	buildSystemPrompt,
	type ContextFile,
	discoverContextFiles,
} from "../context/agents";
import type { MessagePart, UserMultipartMessage } from "../messages/parts";
import {
	appendCompaction,
	appendHandoffSummary,
	appendModelChange,
	appendSessionInfo,
	appendThinkingLevelChange,
	appendTurn,
	createSession,
	deleteSession,
	findSessionById,
	listAllSessions,
	listSessionsForCwd,
	readSession,
	type Session,
	type SessionSummary,
	writeSession,
} from "../session";
import type { KitAgentMessage, Turn } from "../session/types";
import { resolveRetrySettings, type Settings } from "../settings";
import { createDefaultTools } from "../tools";
import { runBash } from "../tools/run-bash";
import { compactSessionTurns, shouldAutoCompact } from "./compaction";
import {
	getRuntimeContextUsage,
	type RuntimeContextUsage,
} from "./context-usage";
import type { GitInfo } from "./git-info";
import { GitInfoWatcher } from "./git-info-watcher";
import { type AgentEvent, KitAgent } from "./kit-agent";
import {
	listRegisteredAuthenticatedProviders,
	resolveDefaultAuthenticatedModel,
} from "./provider-selection";
import { createSyntheticSummaryMessage } from "./session-summary";
import { clampThinkingLevel } from "./thinking-levels";

registerBuiltInApiProviders();

export class AuthenticationRequiredError extends Error {
	constructor(message = "No authenticated providers found. Run /login first.") {
		super(message);
		this.name = "AuthenticationRequiredError";
	}
}

export const DEFAULT_SYSTEM_PROMPT = `You are kit, a coding assistant running in the terminal.
You have access to tools to read and modify files, run commands, search code, and more.
Be concise and direct. Prefer surgical edits over full rewrites when practical.

Kit is customizable through user-owned files and directories. When the user wants to customize Kit itself, prefer those editable surfaces when they fit the request.

Kit customization and documentation:
- Canonical repo reference: https://github.com/akonwi/kit
- When the user asks about Kit itself, its features, settings, themes, skills, prompt commands, context files, or MCP support, inspect this repo's docs/ and relevant source files before making changes.
- Prefer user-editable customization surfaces when they fit the request:
  - Settings: ~/.kit/settings.json
  - Themes: ~/.kit/themes/
  - Global context guidance: ~/.kit/AGENTS.md
  - Project context guidance: AGENTS.md or CLAUDE.md in the working tree
  - User skills: ~/.kit/skills/
  - Project skills: .agents/skills/
  - User prompt commands: ~/.kit/prompts/
  - Project prompt commands: .agents/prompts/
  - MCP config: ~/.kit/mcp.json, .mcp.json, .agents/mcp.json
- Pi compatibility surfaces also exist for some resources:
  - Skills: ~/.pi/agent/skills/
  - Prompt commands: ~/.pi/agent/prompts/
- When customizing Kit behavior for a user, prefer creating or editing those files when they fit the request.
- When working on Kit topics, read the relevant .md files completely and follow cross-references before implementing.`;

const MERGE_UP_SYSTEM_PROMPT = `You are a context summarization assistant.
Do not continue the conversation.
Do not answer the user's requests.
Only produce a compact summary of child-session work that should be merged back into the parent session.`;

export type RuntimeStatus = {
	model: string;
	thinkingLevel: string;
	isStreaming: boolean;
	git: GitInfo;
	contextUsage: RuntimeContextUsage | null;
};

export type RuntimeEventMap = {
	"agent.model.changed": { model: Model<Api>; thinkingLevel: ThinkingLevel };
	"agent.turn.started": { turn: Turn };
	"agent.turn.completed": { turn: Turn | null };
	"user.message.created": {
		turn: Turn;
		message: Extract<KitAgentMessage, { role: "user" }>;
	};
	"bash.command.started": {
		turn: Turn;
		message: Extract<KitAgentMessage, { role: "bashExecution" }>;
	};
	"bash.command.completed": {
		turn: Turn;
		message: Extract<KitAgentMessage, { role: "bashExecution" }>;
	};
	"agent.message.started": {
		turn: Turn;
		message: Extract<AgentMessage, { role: "assistant" }>;
	};
	"agent.message.updated": {
		turn: Turn;
		message: Extract<AgentMessage, { role: "assistant" }>;
	};
	"agent.message.ended": {
		turn: Turn;
		message: Extract<KitAgentMessage, { role: "assistant" }>;
	};
	"agent.thinking.started": { turn: Turn };
	"agent.thinking.updated": { turn: Turn; delta: string };
	"agent.thinking.completed": { turn: Turn };
	"agent.retry.started": {
		attempt: number;
		maxAttempts: number;
		delayMs: number;
	};
	"agent.retry.failed": {
		attempt: number;
		maxAttempts: number;
		error: string;
	};
	"agent.run.failed": { error: string };
	"session.merge.started": Record<string, never>;
	"session.merge.ended": { error?: string };
	"session.compaction.started.auto": { contextPercent: number };
	"session.compaction.completed.auto": {
		contextPercent: number;
		compactedTurnCount: number;
		keptTurnCount: number;
	};
	"session.compaction.failed.auto": {
		error: string;
	};
	"session.compaction.started.recovery": { reason: "overflow" };
	"session.compaction.completed.recovery": {
		reason: "overflow";
		compactedTurnCount: number;
		keptTurnCount: number;
	};
	"session.compaction.failed.recovery": {
		reason: "overflow";
		error: string;
	};
	"session.compaction.started.adaptation": {
		modelId: string;
		modelName: string | undefined;
		contextPercent: number;
	};
	"session.compaction.completed.adaptation": {
		modelId: string;
		modelName: string | undefined;
		compactedTurnCount: number;
		keptTurnCount: number;
	};
	"session.compaction.failed.adaptation": {
		modelId: string;
		modelName: string | undefined;
		cause:
			| "missing-api-key"
			| "cannot-fit"
			| "still-over-capacity"
			| "compaction-error";
		error: string;
	};
	"session.active.changed": { session: Session };
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
	"chat.message-queue.changed": { count: number };
	"chat.followups.promoted": { count: number };
	"settings.changed": { settings: Settings };
	"session.persistence.failed": { error: string };
	"vcs.updated": { branch: string | null; dirty: boolean };
};

export type RuntimeEventName = keyof RuntimeEventMap;

export type AgentRuntimeEvent<K extends RuntimeEventName = RuntimeEventName> =
	K extends RuntimeEventName ? { type: K } & RuntimeEventMap[K] : never;

export type RuntimeEventNameMatchingPrefix<P extends string> = Extract<
	RuntimeEventName,
	`${P}${string}`
>;

export type RuntimeEventPrefixSubscription<P extends string> = {
	prefix: P;
};

export class AgentRuntime {
	private session: Session;
	private agent: KitAgent;
	private _settings: Settings;
	// biome-ignore lint/suspicious/noExplicitAny: heterogeneous tool collection, matches pi-core convention
	private extraTools: AgentTool<any>[];
	private systemPromptAdditions: string[];
	private listeners = new Set<(event: AgentRuntimeEvent) => void>();
	private readonly exactListeners = new Map<
		RuntimeEventName,
		Set<(event: AgentRuntimeEvent) => void>
	>();
	private readonly prefixListeners: Array<{
		prefix: string;
		listener: (event: AgentRuntimeEvent) => void;
	}> = [];
	private quitHandler: (() => void) | null = null;
	private isCompacting = false;
	private unsubscribeAgent: (() => void) | null = null;
	private unsubscribePersistence: (() => void) | null = null;
	private contextFiles: ContextFile[] = [];
	private debugSections = new Map<string, string[]>();
	private gitWatcher: GitInfoWatcher | null = null;
	private gitInfo: GitInfo = { branch: null, dirty: false };
	get vcsInfo() {
		return this.gitInfo;
	}
	private lastSessionModel: string | undefined;
	private overflowRecoveryInFlight = false;
	private lastOverflowRecoveryKey: string | null = null;
	private retryAbortController: AbortController | null = null;
	private retryAttempt = 0;
	private recoveryPromise: Promise<void> | null = null;
	private recoveryResolve: (() => void) | null = null;
	private overflowRecoveryAttempted = false;
	private pendingAutoCompaction: {
		summaryMessage: Extract<KitAgentMessage, { role: "assistant" }>;
		compactedTurnCount: number;
		keptTurnCount: number;
		tokensBefore: number;
		firstKeptTurnId?: string;
	} | null = null;
	// In-memory FIFO buffer of turn ids waiting to be persisted. Survives
	// transient append failures so we never silently drop a completed turn.
	private persistenceQueue: string[] = [];
	private persistenceQueueSet = new Set<string>();
	private isPersistenceFlushInFlight = false;

	constructor(
		session: Session,
		options?: {
			// biome-ignore lint/suspicious/noExplicitAny: heterogeneous tool collection
			extraTools?: AgentTool<any>[];
			systemPromptAdditions?: string[];
			settings?: Settings;
		},
	) {
		this.session = session;
		this._settings = options?.settings ?? {};
		this.lastSessionModel = session.model;
		this.extraTools = options?.extraTools ?? [];
		this.systemPromptAdditions = options?.systemPromptAdditions ?? [];
		const defaultModel = resolveDefaultModel(session.model);
		const initialThinkingLevel = clampThinkingLevel(
			session.thinkingLevel,
			defaultModel,
		);
		this.session = {
			...this.session,
			thinkingLevel: initialThinkingLevel,
		};
		this.contextFiles = discoverContextFiles(session.cwd);
		this.agent = KitAgent.fromSession(session, {
			initialState: {
				model: defaultModel,
				thinkingLevel: initialThinkingLevel,
				systemPrompt: this.getEffectiveSystemPrompt(),
				tools: [...createDefaultTools(session.cwd), ...this.extraTools],
			},
			getApiKey: (provider) => getApiKey(provider),
			maxRetryDelayMs: resolveRetrySettings(this._settings.retry).maxDelayMs,
		});
		this.agent.sessionId = session.id;
		this.unsubscribeAgent = this.agent.subscribe((event) =>
			this.handleAgentEvent(event),
		);
		this.resetGitWatcher();
		this.registerPersistence();
	}

	get contextStats(): RuntimeContextUsage | null {
		return getRuntimeContextUsage(
			this.agent.state.messages,
			this.agent.state.model,
		);
	}

	get settings() {
		return this._settings;
	}

	private registerPersistence(): void {
		this.unsubscribePersistence = this.subscribe((event) => {
			switch (event.type) {
				case "agent.turn.completed":
					// Enqueue every turn from the active session, not just the
					// most recently completed one. A single agent run can create
					// multiple turns (tool-loop iterations, follow-ups), and we
					// must persist all of them; appendTurn is idempotent by id.
					for (const turn of this.session.turns) {
						this.enqueueTurnForPersistence(turn.id);
					}
					this.scheduleFlushPersistence();
					break;
			}
		});
	}

	private touchSession(
		changes: Partial<
			Pick<Session, "name" | "model" | "thinkingLevel" | "turns">
		>,
	): void {
		this.session = {
			...this.session,
			...changes,
			updatedAt: new Date().toISOString(),
		};
	}

	private syncSessionFromAgentState(): void {
		this.touchSession({
			turns: this.agent.turns,
			model: this.agent.state.model?.id,
			thinkingLevel: this.agent.state.thinkingLevel,
		});
	}

	private emitPersistenceFailure(error: unknown): void {
		this.emit("session.persistence.failed", {
			error: error instanceof Error ? error.message : String(error),
		});
	}

	private enqueueTurnForPersistence(turnId: string): void {
		if (this.persistenceQueueSet.has(turnId)) return;
		this.persistenceQueue.push(turnId);
		this.persistenceQueueSet.add(turnId);
	}

	private scheduleFlushPersistence(): void {
		void this.flushPersistenceQueue();
	}

	private async flushPersistenceQueue(): Promise<void> {
		if (this.isPersistenceFlushInFlight) return;
		this.isPersistenceFlushInFlight = true;
		try {
			while (this.persistenceQueue.length > 0) {
				const turnId = this.persistenceQueue[0];
				if (!turnId) break;
				const turn = this.session.turns.find(
					(candidate) => candidate.id === turnId,
				);
				if (!turn) {
					// Stale id (e.g. compaction discarded it); drop and move on.
					this.persistenceQueue.shift();
					this.persistenceQueueSet.delete(turnId);
					continue;
				}
				try {
					await appendTurn(this.session, turn);
					this.persistenceQueue.shift();
					this.persistenceQueueSet.delete(turnId);
				} catch (error) {
					// Keep failed turn at the head for retry on next trigger.
					this.emitPersistenceFailure(error);
					return;
				}
			}
			if (this.pendingAutoCompaction) {
				const pending = this.pendingAutoCompaction;
				try {
					await appendCompaction({
						session: this.session,
						summaryMessage: pending.summaryMessage,
						firstKeptTurnId: pending.firstKeptTurnId,
						compactedTurnCount: pending.compactedTurnCount,
						keptTurnCount: pending.keptTurnCount,
						tokensBefore: pending.tokensBefore,
					});
					if (this.pendingAutoCompaction === pending) {
						this.pendingAutoCompaction = null;
					}
				} catch (error) {
					this.emitPersistenceFailure(error);
				}
			}
		} finally {
			this.isPersistenceFlushInFlight = false;
		}
	}

	private getEffectiveSystemPrompt(): string {
		const basePrompt = [DEFAULT_SYSTEM_PROMPT, ...this.systemPromptAdditions]
			.filter((value) => value.trim().length > 0)
			.join("\n\n");
		return buildSystemPrompt(basePrompt, this.contextFiles);
	}

	private applySessionContext(session: Session): void {
		this.contextFiles = discoverContextFiles(session.cwd);
		this.agent.setSystemPrompt(this.getEffectiveSystemPrompt());
		this.agent.setTools([
			...createDefaultTools(session.cwd),
			...this.extraTools,
		]);
		this.agent.sessionId = session.id;
		this.resetGitWatcher();
	}

	private findModelById(modelId: string | undefined): Model<Api> | undefined {
		if (!modelId) return undefined;
		for (const provider of listRegisteredAuthenticatedProviders(
			getAuthenticatedProviderIds(),
		)) {
			for (const model of getModels(provider)) {
				if (model.id === modelId) return model;
			}
		}
		return undefined;
	}

	private resetGitWatcher(): void {
		this.gitWatcher?.dispose();
		this.gitWatcher = new GitInfoWatcher(this.session.cwd, (gitInfo) => {
			this.gitInfo = gitInfo;
			this.emit("vcs.updated", this.gitInfo);
		});
		this.gitInfo = this.gitWatcher.getCurrent();
	}

	private emit<K extends RuntimeEventName>(
		type: K,
		payload: RuntimeEventMap[K],
	): void {
		const event = { type, ...payload } as AgentRuntimeEvent<K>;
		for (const listener of this.listeners) listener(event);
		const exactListeners = this.exactListeners.get(type);
		if (exactListeners) {
			for (const listener of exactListeners) listener(event);
		}
		for (const { prefix, listener } of this.prefixListeners) {
			if (type.startsWith(prefix)) listener(event);
		}
	}

	private handleSessionChanged(): void {
		const previousModel = this.lastSessionModel;
		this.lastSessionModel = this.session.model;
		if (previousModel !== this.session.model) {
			void this.maybeHandleModelSwitchOverflow();
		}
	}

	private getRetrySettings() {
		return resolveRetrySettings(this._settings.retry);
	}

	private getRestoredThinkingLevel(
		model: Model<Api> | undefined,
	): ThinkingLevel {
		return clampThinkingLevel(this.session.thinkingLevel, model);
	}

	private persistSessionThinkingLevel(level: ThinkingLevel): void {
		if (this.session.thinkingLevel === level) return;
		this.touchSession({ thinkingLevel: level });
		void appendThinkingLevelChange(this.session).catch((error) => {
			this.emitPersistenceFailure(error);
		});
		this.handleSessionChanged();
	}

	private resolveRecovery(): void {
		this.recoveryResolve?.();
		this.recoveryResolve = null;
		this.recoveryPromise = null;
	}

	private async waitForRecovery(): Promise<void> {
		if (this.recoveryPromise) {
			await this.recoveryPromise;
		}
	}

	private findLastAssistantMessage(
		messages: AgentMessage[],
	): Extract<AgentMessage, { role: "assistant" }> | undefined {
		for (let index = messages.length - 1; index >= 0; index--) {
			const message = messages[index];
			if (message.role === "assistant") return message;
		}
		return undefined;
	}

	private isContextOverflowError(
		message: Extract<AgentMessage, { role: "assistant" }>,
	): boolean {
		if (message.stopReason !== "error" || !message.errorMessage) return false;
		return /prompt is too long|exceeds the context window|input token count.*exceeds|maximum prompt length is|reduce the length of the messages|maximum context length|available context size|greater than the context length|context window exceeds limit|exceeded model token limit|too large for model with .* maximum context length/i.test(
			message.errorMessage,
		);
	}

	private isRetryableAssistantError(
		message: Extract<AgentMessage, { role: "assistant" }>,
	): boolean {
		if (message.stopReason !== "error" || !message.errorMessage) return false;
		if (this.isContextOverflowError(message)) return false;
		return /overloaded|provider.?returned.?error|rate.?limit|too many requests|429|500|502|503|504|service.?unavailable|server.?error|internal.?error|network.?error|connection.?error|connection.?refused|other side closed|fetch failed|upstream.?connect|reset before headers|socket hang up|timed? out|timeout|terminated|retry delay/i.test(
			message.errorMessage,
		);
	}

	private createRecoveryPromiseForAgentEnd(event: AgentEvent): void {
		if (event.type !== "agent_end" || this.recoveryPromise) return;
		const assistant = this.findLastAssistantMessage(event.messages);
		if (!assistant) return;
		if (
			this.isContextOverflowError(assistant) ||
			(this.getRetrySettings().enabled &&
				this.isRetryableAssistantError(assistant))
		) {
			this.recoveryPromise = new Promise<void>((resolve) => {
				this.recoveryResolve = resolve;
			});
		}
	}

	private syncPendingState() {
		const count = this.agent.getPendingFollowUps().length;
		this.emit("chat.message-queue.changed", { count });
	}

	private snapshotStatus(): RuntimeStatus {
		return {
			model:
				this.agent.state.model?.name ??
				this.agent.state.model?.id ??
				"no model",
			thinkingLevel: this.agent.state.thinkingLevel ?? "off",
			isStreaming: this.agent.state.isStreaming,
			git: this.gitInfo,
			contextUsage: getRuntimeContextUsage(
				this.agent.state.messages,
				this.agent.state.model,
			),
		};
	}

	private async maybeAutoCompact() {
		if (this.isCompacting || this.overflowRecoveryInFlight) return;
		const model = this.agent.state.model;
		const contextUsage = getRuntimeContextUsage(
			this.agent.state.messages,
			model,
		);
		if (!model || !shouldAutoCompact(contextUsage?.percent)) return;
		const apiKey = await getApiKey(model.provider);
		if (!apiKey) {
			this.emit("session.compaction.failed.auto", {
				error: `No API key available for ${model.provider}.`,
			});
			return;
		}

		this.isCompacting = true;
		this.emit("session.compaction.started.auto", {
			contextPercent: contextUsage?.percent ?? 0,
		});

		try {
			const result = await compactSessionTurns({
				session: this.session,
				model,
				apiKey,
			});
			if (!result) return;

			this.agent.replaceFromTurns(result.turns);
			this.touchSession({
				turns: result.turns,
				model: model.id,
				thinkingLevel: this.agent.state.thinkingLevel,
			});
			this.pendingAutoCompaction = {
				summaryMessage: result.summaryMessage as Extract<
					KitAgentMessage,
					{ role: "assistant" }
				>,
				compactedTurnCount: result.compactedTurnCount,
				keptTurnCount: result.keptTurnCount,
				tokensBefore: result.tokensBefore,
				firstKeptTurnId: result.turns.at(1)?.id,
			};
			this.handleSessionChanged();
			this.emit("session.compaction.completed.auto", {
				contextPercent: contextUsage?.percent ?? 0,
				compactedTurnCount: result.compactedTurnCount,
				keptTurnCount: result.keptTurnCount,
			});
		} catch (error) {
			this.emit("session.compaction.failed.auto", {
				error: error instanceof Error ? error.message : String(error),
			});
		} finally {
			this.isCompacting = false;
		}
	}

	private persistTurns(): boolean {
		this.syncSessionFromAgentState();
		return true;
	}

	private async persistKeptTurnsForCompaction(turns: Turn[]): Promise<void> {
		for (const turn of turns.slice(1)) {
			await appendTurn(this.session, turn);
		}
	}

	private removeTerminalAssistantErrorFromLiveState(): void {
		const turns = [...this.agent.turns];
		const lastTurn = turns.at(-1);
		const lastMessage = lastTurn?.messages.at(-1);
		if (!lastTurn || lastMessage?.role !== "assistant") return;
		if (lastMessage.stopReason !== "error") return;
		const nextTurns = turns.slice(0, -1);
		const nextLastTurn: Turn = {
			...lastTurn,
			messages: lastTurn.messages.slice(0, -1),
		};
		if (nextLastTurn.messages.length > 0) {
			nextTurns.push(nextLastTurn);
		}
		this.agent.replaceFromTurns(nextTurns);
		this.emit("session.active.changed", { session: this.session });
	}

	private scheduleContinue(): void {
		setTimeout(() => {
			void this.agent.continue().catch((error) => {
				this.emit("agent.retry.failed", {
					attempt: this.retryAttempt,
					maxAttempts: this.getRetrySettings().maxRetries,
					error: error instanceof Error ? error.message : String(error),
				});
				this.retryAttempt = 0;
				this.retryAbortController = null;
				this.overflowRecoveryAttempted = false;
				this.resolveRecovery();
			});
		}, 0);
	}

	private async sleepWithAbort(ms: number, signal: AbortSignal): Promise<void> {
		await new Promise<void>((resolve, reject) => {
			if (signal.aborted) {
				reject(new Error("aborted"));
				return;
			}
			const timeout = setTimeout(() => {
				signal.removeEventListener("abort", onAbort);
				resolve();
			}, ms);
			const onAbort = () => {
				clearTimeout(timeout);
				reject(new Error("aborted"));
			};
			signal.addEventListener("abort", onAbort, { once: true });
		});
	}

	private async handleRetryableAssistantError(): Promise<boolean> {
		const settings = this.getRetrySettings();
		if (!settings.enabled) return false;
		this.retryAttempt += 1;
		if (this.retryAttempt > settings.maxRetries) {
			this.retryAttempt = 0;
			this.resolveRecovery();
			return false;
		}

		this.removeTerminalAssistantErrorFromLiveState();
		const delayMs = settings.baseDelayMs * 2 ** (this.retryAttempt - 1);
		this.emit("agent.retry.started", {
			attempt: this.retryAttempt,
			maxAttempts: settings.maxRetries,
			delayMs,
		});
		this.retryAbortController = new AbortController();
		try {
			await this.sleepWithAbort(delayMs, this.retryAbortController.signal);
		} catch {
			this.emit("agent.retry.failed", {
				attempt: this.retryAttempt,
				maxAttempts: settings.maxRetries,
				error: "Retry cancelled before continue.",
			});
			this.retryAttempt = 0;
			this.retryAbortController = null;
			this.resolveRecovery();
			return true;
		}
		this.retryAbortController = null;
		this.scheduleContinue();
		return true;
	}

	private async handleOverflowAssistantError(
		message: Extract<AgentMessage, { role: "assistant" }>,
	): Promise<boolean> {
		const model = this.agent.state.model;
		if (!model) return false;
		if (this.overflowRecoveryAttempted) {
			this.emit("session.compaction.failed.recovery", {
				reason: "overflow",
				error: [
					"Kit already attempted one compact-and-retry recovery for this overflow.",
					message.errorMessage ??
						"Start a new session or switch to a larger-context model.",
				].join(" "),
			});
			this.overflowRecoveryAttempted = false;
			this.resolveRecovery();
			return false;
		}

		const apiKey = await getApiKey(model.provider);
		if (!apiKey) return false;

		this.overflowRecoveryAttempted = true;
		this.removeTerminalAssistantErrorFromLiveState();
		this.emit("session.compaction.started.recovery", {
			reason: "overflow",
		});
		try {
			const result = await compactSessionTurns({
				session: this.session,
				model,
				apiKey,
			});
			if (!result) {
				this.resolveRecovery();
				return false;
			}
			this.agent.replaceFromTurns(result.turns);
			this.touchSession({
				turns: result.turns,
				model: model.id,
				thinkingLevel: this.agent.state.thinkingLevel,
			});
			await this.persistKeptTurnsForCompaction(result.turns);
			await appendCompaction({
				session: this.session,
				summaryMessage: result.summaryMessage as Extract<
					KitAgentMessage,
					{ role: "assistant" }
				>,
				firstKeptTurnId: result.turns.at(1)?.id,
				compactedTurnCount: result.compactedTurnCount,
				keptTurnCount: result.keptTurnCount,
				tokensBefore: result.tokensBefore,
			});
			this.handleSessionChanged();
			this.emit("session.compaction.completed.recovery", {
				reason: "overflow",
				compactedTurnCount: result.compactedTurnCount,
				keptTurnCount: result.keptTurnCount,
			});
			this.scheduleContinue();
			return true;
		} catch (error) {
			this.emit("session.compaction.failed.recovery", {
				reason: "overflow",
				error: error instanceof Error ? error.message : String(error),
			});
			this.resolveRecovery();
			return false;
		}
	}

	private async finalizeAgentRun(messages: AgentMessage[]): Promise<void> {
		this.persistTurns();
		const assistant = this.findLastAssistantMessage(messages);
		if (assistant?.stopReason === "error") {
			if (this.isContextOverflowError(assistant)) {
				const didRecover = await this.handleOverflowAssistantError(assistant);
				if (didRecover) return;
			}
			if (this.isRetryableAssistantError(assistant)) {
				const didRetry = await this.handleRetryableAssistantError();
				if (didRetry) return;
			}
		}
		await this.maybeAutoCompact();
		this.handleSessionChanged();
		this.emit("agent.turn.completed", {
			turn: this.agent.turns.at(-1) ?? null,
		});
		this.syncPendingState();
		this.retryAttempt = 0;
		this.overflowRecoveryAttempted = false;
		this.resolveRecovery();
	}

	private async maybeHandleModelSwitchOverflow(): Promise<void> {
		const model = this.agent.state.model;
		if (!model || this.isCompacting || this.overflowRecoveryInFlight) return;

		const contextUsage = getRuntimeContextUsage(
			this.agent.state.messages,
			model,
		);
		if (!contextUsage || contextUsage.percent <= 100) return;

		const recoveryKey = `${this.session.id}:${model.id}:${this.session.updatedAt}:${this.agent.turns.length}`;
		if (this.lastOverflowRecoveryKey === recoveryKey) return;
		this.lastOverflowRecoveryKey = recoveryKey;

		const apiKey = await getApiKey(model.provider);
		if (!apiKey) {
			this.emit("session.compaction.failed.adaptation", {
				modelId: model.id,
				modelName: model.name,
				cause: "missing-api-key",
				error: `No API key available for ${model.provider} to compact this session for ${model.name ?? model.id}.`,
			});
			return;
		}

		this.overflowRecoveryInFlight = true;
		this.emit("session.compaction.started.adaptation", {
			modelId: model.id,
			modelName: model.name,
			contextPercent: contextUsage.percent,
		});

		try {
			const result = await compactSessionTurns({
				session: this.session,
				model,
				apiKey,
			});
			if (!result) {
				this.emit("session.compaction.failed.adaptation", {
					modelId: model.id,
					modelName: model.name,
					cause: "cannot-fit",
					error: `${model.name ?? model.id} cannot fit this session, even after optimized compaction.`,
				});
				return;
			}

			this.agent.replaceFromTurns(result.turns);
			this.touchSession({
				turns: result.turns,
				model: model.id,
				thinkingLevel: this.agent.state.thinkingLevel,
			});
			await this.persistKeptTurnsForCompaction(result.turns);
			await appendCompaction({
				session: this.session,
				summaryMessage: result.summaryMessage as Extract<
					KitAgentMessage,
					{ role: "assistant" }
				>,
				firstKeptTurnId: result.turns.at(1)?.id,
				compactedTurnCount: result.compactedTurnCount,
				keptTurnCount: result.keptTurnCount,
				tokensBefore: result.tokensBefore,
			});
			this.handleSessionChanged();

			this.emit("session.compaction.completed.adaptation", {
				modelId: model.id,
				modelName: model.name,
				compactedTurnCount: result.compactedTurnCount,
				keptTurnCount: result.keptTurnCount,
			});
			const nextUsage = getRuntimeContextUsage(
				this.agent.state.messages,
				model,
			);
			if (nextUsage && nextUsage.percent > 100) {
				this.emit("session.compaction.failed.adaptation", {
					modelId: model.id,
					modelName: model.name,
					cause: "still-over-capacity",
					error: `${model.name ?? model.id} is still over capacity after compaction (${nextUsage.percent}%).`,
				});
			}
		} catch (error) {
			this.emit("session.compaction.failed.adaptation", {
				modelId: model.id,
				modelName: model.name,
				cause: "compaction-error",
				error: error instanceof Error ? error.message : String(error),
			});
		} finally {
			this.overflowRecoveryInFlight = false;
		}
	}

	private handleAgentEvent(event: AgentEvent) {
		this.createRecoveryPromiseForAgentEnd(event);
		switch (event.type) {
			case "agent_start":
				break;

			case "turn_start":
				this.syncPendingState();
				this.emit("agent.turn.started", { turn: event.turn });
				break;

			case "turn_end":
				break;

			case "user_message_created":
				this.emit("user.message.created", {
					turn: event.turn,
					message: event.message,
				});
				break;

			case "assistant_message_started":
				this.emit("agent.message.started", {
					turn: event.turn,
					message: event.message,
				});
				break;

			case "assistant_message_updated":
				this.emit("agent.message.updated", {
					turn: event.turn,
					message: event.message,
				});
				break;

			case "assistant_message_ended":
				this.emit("agent.message.ended", {
					turn: event.turn,
					message: event.message,
				});
				break;

			case "agent_thinking_started":
				this.emit("agent.thinking.started", { turn: event.turn });
				break;

			case "agent_thinking_updated":
				this.emit("agent.thinking.updated", {
					turn: event.turn,
					delta: event.delta,
				});
				break;

			case "agent_thinking_completed":
				this.emit("agent.thinking.completed", { turn: event.turn });
				break;

			case "message_end":
				break;

			case "agent_tool_started":
				this.emit("agent.tool.started", {
					turn: event.turn,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
				});
				break;

			case "agent_tool_updated":
				this.emit("agent.tool.updated", {
					turn: event.turn,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					partialResult: event.partialResult,
				});
				break;

			case "agent_tool_ended":
				this.emit("agent.tool.ended", {
					turn: event.turn,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					result: event.result,
					isError: event.isError,
				});
				break;

			case "agent_end":
				void this.finalizeAgentRun(event.messages).catch((error) => {
					this.retryAttempt = 0;
					this.overflowRecoveryAttempted = false;
					this.resolveRecovery();
					this.emit("agent.run.failed", {
						error: error instanceof Error ? error.message : String(error),
					});
				});
				break;
		}
	}

	async submitMessage(input: string | MessagePart[]): Promise<void> {
		const parts: MessagePart[] =
			typeof input === "string" ? [{ type: "text", text: input }] : input;
		if (!this.agent.state.isStreaming) {
			await this.submitUserMessage(parts);
			return;
		}
		const textOnly = parts.every((part) => part.type === "text");
		if (!textOnly) {
			throw new Error(
				"Attachments not supported in queued follow-ups. Wait for the current turn to finish before sending attached reviews.",
			);
		}
		const queuedText = parts
			.map((part) => (part.type === "text" ? part.text : ""))
			.join("\n")
			.trim();
		if (!queuedText) return;
		this.sendFollowUp(queuedText);
	}

	async submitUserMessage(input: string | MessagePart[]): Promise<void> {
		const parts: MessagePart[] =
			typeof input === "string" ? [{ type: "text", text: input }] : input;
		const message: UserMultipartMessage = {
			role: "user",
			content: parts,
			timestamp: Date.now(),
		};
		await this.agent.prompt(message as unknown as AgentMessage);
		await this.waitForRecovery();
	}

	async submitPromptCommandMessage(
		command: string,
		args: string,
		expandedPrompt: string,
	): Promise<void> {
		const promptText = expandedPrompt.trim();
		if (!promptText) return;

		const message: UserMultipartMessage & {
			synthetic: {
				kind: "prompt-command";
				command: string;
				args?: string;
			};
		} = {
			role: "user",
			content: [{ type: "text", text: promptText }],
			timestamp: Date.now(),
			synthetic: {
				kind: "prompt-command",
				command,
				...(args.trim().length > 0 ? { args: args.trim() } : {}),
			},
		};

		if (this.agent.state.isStreaming) {
			this.agent.followUp(message as unknown as AgentMessage);
			this.syncPendingState();
			return;
		}

		await this.agent.prompt(message as unknown as AgentMessage);
		await this.waitForRecovery();
	}

	abort(): void {
		this.retryAbortController?.abort();
		this.agent.abort();
	}

	addTool(tool: AgentTool): () => void {
		this.extraTools.push(tool);
		this.agent.setTools([
			...createDefaultTools(this.session.cwd),
			...this.extraTools,
		]);
		return () => {
			this.extraTools = this.extraTools.filter(
				(candidate) => candidate !== tool,
			);
			this.agent.setTools([
				...createDefaultTools(this.session.cwd),
				...this.extraTools,
			]);
		};
	}

	/**
	 * Append text to the effective system prompt and propagate it to the agent.
	 * Intended for plugins that own a feature-specific policy or tool guidelines
	 * that should be part of the system prompt without being baked into `App.tsx`.
	 */
	addSystemPromptAddition(text: string): () => void {
		const trimmed = text.trim();
		if (!trimmed) return () => {};
		this.systemPromptAdditions.push(trimmed);
		this.agent.setSystemPrompt(this.getEffectiveSystemPrompt());
		return () => {
			const index = this.systemPromptAdditions.indexOf(trimmed);
			if (index < 0) return;
			this.systemPromptAdditions.splice(index, 1);
			this.agent.setSystemPrompt(this.getEffectiveSystemPrompt());
		};
	}

	/**
	 * Execute a bash command from the user's `!` prefix.
	 * Injects a synthetic bashExecution message into the transcript.
	 * When excludeFromContext is true (`!!`), the message is not
	 * sent to the model on the next turn.
	 */
	async executeBash(
		command: string,
		excludeFromContext = false,
	): Promise<void> {
		const id = randomUUID();
		const timestamp = Date.now();
		const pendingMessage: AgentMessage = {
			role: "bashExecution",
			id,
			command,
			output: "",
			exitCode: undefined,
			cancelled: false,
			truncated: false,
			pending: true,
			excludeFromContext: true,
			timestamp,
		};
		const appended = this.agent.appendCustomMessage(pendingMessage);
		this.emit("bash.command.started", {
			turn: appended.turn,
			message: appended.message as Extract<
				KitAgentMessage,
				{ role: "bashExecution" }
			>,
		});

		const result = await runBash(command, this.session.cwd);

		const bashMessage: AgentMessage = {
			role: "bashExecution",
			id,
			command,
			output: result.output,
			exitCode: result.exitCode,
			cancelled: false,
			truncated: false,
			pending: false,
			excludeFromContext,
			timestamp,
		};
		const replaced = this.agent.replaceCustomMessage(
			(message) =>
				message.role === "bashExecution" &&
				"id" in message &&
				message.id === id,
			bashMessage,
		);
		if (replaced) {
			this.syncSessionFromAgentState();
			this.emit("bash.command.completed", {
				turn: replaced.turn,
				message: replaced.message as Extract<
					KitAgentMessage,
					{ role: "bashExecution" }
				>,
			});
		}
	}

	sendFollowUp(text: string): void {
		const msg: UserMessage = {
			role: "user",
			content: text,
			timestamp: Date.now(),
		};
		this.agent.followUp(msg);
		this.syncPendingState();
	}

	sendSteer(text: string): void {
		const msg: UserMessage = {
			role: "user",
			content: text,
			timestamp: Date.now(),
		};
		this.agent.steer(msg);
	}

	clearPendingMessages(): void {
		this.agent.clearPendingFollowUps();
		this.syncPendingState();
	}

	drainPendingMessages(): string[] {
		const drained = this.agent.drainPendingFollowUps();
		this.syncPendingState();
		return drained;
	}

	promotePendingFollowUpsToSteering(): void {
		const drained = this.agent.drainPendingFollowUps();
		for (const text of drained) {
			const msg: UserMessage = {
				role: "user",
				content: text,
				timestamp: Date.now(),
			};
			this.agent.steer(msg);
		}
		this.syncPendingState();
		if (drained.length > 0) {
			this.emit("chat.followups.promoted", { count: drained.length });
		}
	}

	getPendingMessageCount(): number {
		return this.agent.getPendingFollowUps().length;
	}

	getPendingMessages(): string[] {
		return this.agent.getPendingFollowUps();
	}

	getSession(): Session {
		return this.session;
	}

	getContextFiles(): ContextFile[] {
		return [...this.contextFiles];
	}

	/**
	 * Register a named section of debug lines.
	 * Plugins call this so `/debug` can display their state.
	 */
	setDebugSection(key: string, lines: string[]): () => void {
		this.debugSections.set(key, lines);
		return () => {
			this.debugSections.delete(key);
		};
	}

	getDebugSections(): Map<string, string[]> {
		return this.debugSections;
	}

	getMessages(): AgentMessage[] {
		return this.agent.turns.flatMap((turn) => turn.messages);
	}

	getTools(): AgentTool[] {
		return [...createDefaultTools(this.session.cwd), ...this.extraTools];
	}

	getSystemPromptAdditions(): string[] {
		return [...this.systemPromptAdditions];
	}

	getTurns(): Turn[] {
		return [...this.agent.turns];
	}

	async newSession(cwd?: string): Promise<void> {
		const targetCwd = cwd ?? this.session.cwd;
		this.session = await createSession(
			targetCwd,
			this.agent.state.model?.id,
			this.agent.state.thinkingLevel,
		);
		this.agent.replaceFromTurns([]);
		const restoredThinkingLevel = this.getRestoredThinkingLevel(
			this.agent.state.model,
		);
		this.agent.setThinkingLevel(restoredThinkingLevel);
		this.session.thinkingLevel = restoredThinkingLevel;
		this.applySessionContext(this.session);
		this.syncPendingState();
		this.emit("session.active.changed", { session: this.session });
		this.handleSessionChanged();
	}

	async handoffSession(firstMessage?: string): Promise<Session> {
		if (this.session.turns.length === 0) {
			throw new Error("Nothing to hand off yet.");
		}

		const parentName = this.session.name?.trim() || "Untitled";
		const forkedFromTurnId = this.session.turns.at(-1)?.id;
		const now = new Date().toISOString();
		const child: Session = {
			...(await createSession(
				this.session.cwd,
				this.agent.state.model?.id,
				this.agent.state.thinkingLevel,
			)),
			parentSessionId: this.session.id,
			forkedFromTurnId,
			name: `handoff: ${parentName}`,
			model: this.agent.state.model?.id ?? this.session.model,
			thinkingLevel: this.agent.state.thinkingLevel,
			createdAt: now,
			updatedAt: now,
			turns: structuredClone(this.session.turns),
		};

		this.session = child;
		this.agent.replaceFromTurns(child.turns);
		const restoredThinkingLevel = this.getRestoredThinkingLevel(
			this.agent.state.model,
		);
		this.agent.setThinkingLevel(restoredThinkingLevel);
		this.session = { ...this.session, thinkingLevel: restoredThinkingLevel };
		// Handoff creates a new child session pre-seeded with copied history.
		// Persist it immediately so the child exists on disk even before the
		// next appended turn or metadata event.
		await writeSession(this.session);
		this.applySessionContext(child);
		this.syncPendingState();
		this.handleSessionChanged();
		this.emit("session.active.changed", { session: this.session });

		const prompt = firstMessage?.trim();
		if (prompt) {
			await this.submitUserMessage(prompt);
		}

		return child;
	}

	async switchSession(id: string): Promise<boolean> {
		const target = (await findSessionById(id)) ?? (await readSession(id));
		if (!target) return false;
		this.session = target;
		this.agent.replaceFromTurns(this.session.turns);
		const model = this.findModelById(this.session.model);
		if (model) this.agent.setModel(model);
		const restoredThinkingLevel = this.getRestoredThinkingLevel(
			this.agent.state.model,
		);
		this.agent.setThinkingLevel(restoredThinkingLevel);
		this.session = { ...this.session, thinkingLevel: restoredThinkingLevel };
		this.applySessionContext(this.session);
		this.syncPendingState();
		this.handleSessionChanged();
		this.emit("session.active.changed", { session: this.session });
		return true;
	}

	async mergeUp(): Promise<void> {
		this.emit("session.merge.started", {});
		try {
			const child = this.session;
			if (!child.parentSessionId) {
				throw new Error("Current session is not a child session.");
			}
			const parent =
				(await findSessionById(child.parentSessionId)) ??
				(await readSession(child.parentSessionId));
			if (!parent) {
				throw new Error("Parent session could not be found.");
			}

			const currentModel = this.getCurrentModel();
			if (!currentModel) {
				throw new Error("No active model available for merge summary.");
			}
			const apiKey = await getApiKey(currentModel.provider);
			if (!apiKey) {
				throw new Error(`No API key available for ${currentModel.provider}.`);
			}

			const boundaryIndex = child.forkedFromTurnId
				? child.turns.findIndex((turn) => turn.id === child.forkedFromTurnId)
				: -1;
			const turnsToSummarize =
				boundaryIndex >= 0 ? child.turns.slice(boundaryIndex + 1) : child.turns;
			const messagesToSummarize = turnsToSummarize.flatMap(
				(turn) => turn.messages,
			);
			if (messagesToSummarize.length === 0) {
				throw new Error("Nothing new to merge from this child session.");
			}

			const mergePrompt = [
				"Summarize this child session so its useful work can be merged back into the parent session.",
				boundaryIndex >= 0
					? "The conversation below contains only work that happened after the child branched from the parent."
					: "The original fork boundary could not be found, so the conversation below contains the full child session.",
				child.name?.trim()
					? `Child session name: ${child.name.trim()}`
					: undefined,
				"",
				"Use this exact structure:",
				"",
				"## Branch goal",
				"[what this side quest was trying to accomplish]",
				"",
				"## Progress / outcomes",
				"- [important progress made in the child session]",
				"",
				"## Key decisions / changes",
				"- [important decisions, code changes, or findings the parent should know]",
				"",
				"## Remaining issues",
				"- [unfinished work, risks, or follow-up items]",
				"",
				"## Context to preserve",
				"- [specific details the parent should retain before continuing]",
				"",
				"Be concise but specific. Focus on what the parent needs to resume accurately.",
			]
				.filter((line): line is string => typeof line === "string")
				.join("\n");

			const summaryMessage = await createSyntheticSummaryMessage({
				messages: messagesToSummarize,
				model: currentModel,
				apiKey,
				systemPrompt: MERGE_UP_SYSTEM_PROMPT,
				userPrompt: mergePrompt,
				kind: "handoff-summary",
				sourceSessionName: child.name?.trim() || undefined,
			});
			const summaryTurn: Turn = {
				id: summaryMessage.turnId,
				messages: [summaryMessage],
			};

			const switched = await this.switchSession(parent.id);
			if (!switched) {
				throw new Error("Failed to switch back to the parent session.");
			}

			this.agent.replaceFromTurns([...this.session.turns, summaryTurn]);
			this.syncSessionFromAgentState();
			await appendHandoffSummary(this.session, summaryMessage);
			this.handleSessionChanged();
			this.emit("session.active.changed", { session: this.session });

			await deleteSession(child.id);
			this.emit("session.merge.ended", {});
		} catch (error) {
			this.emit("session.merge.ended", {
				error: error instanceof Error ? error.message : String(error),
			});
			throw error;
		}
	}

	async reloadSession() {
		this.applySessionContext(this.session);
	}

	async setSessionName(name: string): Promise<void> {
		if (this.session.name === name) return;
		this.touchSession({ name });
		try {
			await appendSessionInfo(this.session, name);
		} catch (error) {
			this.emitPersistenceFailure(error);
		}
		this.emit("session.active.changed", { session: this.session });
		this.handleSessionChanged();
	}

	async listAllSessions(): Promise<SessionSummary[]> {
		return listAllSessions();
	}

	async listSessionsForCwd(cwd: string): Promise<SessionSummary[]> {
		return listSessionsForCwd(cwd);
	}

	async deleteSession(id: string): Promise<void> {
		if (id === this.session.id) {
			throw new Error("Cannot delete the active session");
		}
		await deleteSession(id);
	}

	getStatus(): RuntimeStatus {
		return this.snapshotStatus();
	}

	getAvailableModels(): Array<Model<Api>> {
		return listRegisteredAuthenticatedProviders(
			getAuthenticatedProviderIds(),
		).flatMap((provider) => getModels(provider));
	}

	getCurrentModelId(): string | undefined {
		return this.agent.state.model?.id;
	}

	getCurrentModel(): Model<Api> | undefined {
		return this.agent.state.model;
	}

	setModel(model: Model<Api>): void {
		this.agent.setModel(model);
		this.touchSession({ model: model.id });
		void appendModelChange(this.session).catch((error) => {
			this.emitPersistenceFailure(error);
		});
		this.emit("agent.model.changed", {
			model,
			thinkingLevel: this.agent.state.thinkingLevel,
		});
		this.handleSessionChanged();
	}

	setThinkingLevel(level: ThinkingLevel): void {
		const clamped = clampThinkingLevel(level, this.agent.state.model);
		this.agent.setThinkingLevel(clamped);
		this.persistSessionThinkingLevel(clamped);
		this.emit("agent.model.changed", {
			model: this.agent.state.model,
			thinkingLevel: this.agent.state.thinkingLevel,
		});
	}

	get agentInfo() {
		return {
			model: this.agent.state.model,
			thinkingLevel: this.agent.state.thinkingLevel,
		};
	}

	emitSettingsChanged(settings: Settings): void {
		this._settings = settings;
		this.agent.maxRetryDelayMs = this.getRetrySettings().maxDelayMs;
		this.emit("settings.changed", { settings });
	}

	onQuit(handler: () => void): void {
		this.quitHandler = handler;
	}

	quit(): void {
		this.quitHandler?.();
	}

	subscribe(listener: (event: AgentRuntimeEvent) => void): () => void;
	subscribe<K extends RuntimeEventName>(
		type: K,
		listener: (event: AgentRuntimeEvent<K>) => void,
	): () => void;
	subscribe<P extends string>(
		options: RuntimeEventPrefixSubscription<P>,
		listener: (
			event: AgentRuntimeEvent<RuntimeEventNameMatchingPrefix<P>>,
		) => void,
	): () => void;
	subscribe<K extends RuntimeEventName, P extends string>(
		typeOrListener:
			| K
			| RuntimeEventPrefixSubscription<P>
			| ((event: AgentRuntimeEvent) => void),
		maybeListener?:
			| ((event: AgentRuntimeEvent<K>) => void)
			| ((event: AgentRuntimeEvent<RuntimeEventNameMatchingPrefix<P>>) => void),
	): () => void {
		if (typeof typeOrListener === "function") {
			const listener = typeOrListener;
			this.listeners.add(listener);
			return () => this.listeners.delete(listener);
		}

		const listener = maybeListener as (event: AgentRuntimeEvent) => void;
		if (typeof typeOrListener === "object" && typeOrListener !== null) {
			const entry = {
				prefix: typeOrListener.prefix,
				listener,
			};
			this.prefixListeners.push(entry);
			return () => {
				const index = this.prefixListeners.indexOf(entry);
				if (index >= 0) this.prefixListeners.splice(index, 1);
			};
		}

		const type = typeOrListener;
		const listeners = this.exactListeners.get(type) ?? new Set();
		listeners.add(listener);
		this.exactListeners.set(type, listeners);
		return () => {
			const current = this.exactListeners.get(type);
			if (!current) return;
			current.delete(listener);
			if (current.size === 0) this.exactListeners.delete(type);
		};
	}

	dispose(): void {
		this.unsubscribeAgent?.();
		this.unsubscribeAgent = null;
		this.unsubscribePersistence?.();
		this.unsubscribePersistence = null;
		this.gitWatcher?.dispose();
		this.gitWatcher = null;
		this.listeners.clear();
		this.exactListeners.clear();
		this.prefixListeners.length = 0;
	}
}

function resolveDefaultModel(preferredModelId?: string): Model<Api> {
	const model = resolveDefaultAuthenticatedModel(
		getAuthenticatedProviderIds(),
		preferredModelId,
	);
	if (!model) {
		throw new AuthenticationRequiredError();
	}
	return model;
}
