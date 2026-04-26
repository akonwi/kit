import { randomUUID } from "node:crypto";
import "./custom-messages";
import type {
	AgentEvent,
	AgentMessage,
	AgentTool,
	ThinkingLevel,
} from "@mariozechner/pi-agent-core";
import {
	type Api,
	getEnvApiKey,
	getModels,
	getProviders,
	type KnownProvider,
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
	createSession,
	deleteSession,
	findSessionById,
	listAllSessions,
	listSessionsForCwd,
	readSession,
	type Session,
	type SessionSummary,
	updateSession,
	writeSession,
} from "../session";
import type { Turn } from "../session/types";
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
import { KitAgent } from "./kit-agent";
import { createSyntheticSummaryMessage } from "./session-summary";
import { clampThinkingLevel } from "./thinking-levels";

registerBuiltInApiProviders();

const DEFAULT_SYSTEM_PROMPT = `You are kit, a coding assistant running in the terminal.
You have access to tools to read and modify files, run commands, search code, and more.
Be concise and direct. Prefer surgical edits over full rewrites when practical.`;

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

export type RuntimePanelState = {
	pending: boolean;
	title: string;
};

export type RuntimeEventMap = {
	"session.turns.changed": { turns: Turn[] };
	"runtime.status.changed": { status: RuntimeStatus };
	"session.changed": { session: Session };
	"session.updated": { session: Session };
	"session.name.changed": { name: string };
	"session.updated.model": { session: Session; modelId: string | undefined };
	"runtime.updated.git": { git: GitInfo; status: RuntimeStatus };
	"runtime.panel.changed": { panel: RuntimePanelState };
	"tool.completed": Record<string, never>;
	"turn.completed": { turn: Turn | null };
	"runtime.pending.changed": { count: number };
	"runtime.pending.messages.changed": { messages: string[] };
	"settings.changed": { settings: Settings };
	"notification.error": { title: string; lines: string[] };
	"notification.warning": { title: string; lines: string[] };
	"notification.info": { title: string; lines: string[] };
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
	private settings: Settings;
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
	private pendingCount = 0;
	private isCompacting = false;
	private unsubscribeAgent: (() => void) | null = null;
	private contextFiles: ContextFile[] = [];
	private debugSections = new Map<string, string[]>();
	private gitWatcher: GitInfoWatcher | null = null;
	private gitInfo: GitInfo = { branch: null, dirty: false };
	private lastSessionModel: string | undefined;
	private lastSessionName: string | undefined;
	private overflowRecoveryInFlight = false;
	private lastOverflowRecoveryKey: string | null = null;
	private retryAbortController: AbortController | null = null;
	private retryAttempt = 0;
	private recoveryPromise: Promise<void> | null = null;
	private recoveryResolve: (() => void) | null = null;
	private overflowRecoveryAttempted = false;

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
		this.settings = options?.settings ?? {};
		this.lastSessionModel = session.model;
		this.lastSessionName = session.name;
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

		console.log(
			"[runtime] model:",
			defaultModel.id,
			"provider:",
			defaultModel.provider,
			"api:",
			defaultModel.api,
		);

		this.agent = KitAgent.fromSession(session, {
			initialState: {
				model: defaultModel,
				thinkingLevel: initialThinkingLevel,
				systemPrompt: this.getEffectiveSystemPrompt(),
				tools: [...createDefaultTools(session.cwd), ...this.extraTools],
			},
			getApiKey: (provider) => getApiKey(provider),
			maxRetryDelayMs: resolveRetrySettings(this.settings.retry).maxDelayMs,
		});
		this.agent.sessionId = session.id;
		this.unsubscribeAgent = this.agent.subscribe((event) =>
			this.handleAgentEvent(event),
		);
		this.resetGitWatcher();
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
		for (const provider of getAuthenticatedProviders()) {
			for (const model of getModels(provider as KnownProvider)) {
				if (model.id === modelId) return model;
			}
		}
		return undefined;
	}

	private resetGitWatcher(): void {
		this.gitWatcher?.dispose();
		this.gitWatcher = new GitInfoWatcher(this.session.cwd, (git) => {
			this.gitInfo = git;
			const status = this.snapshotStatus();
			this.emit("runtime.updated.git", { git, status });
			this.emit("runtime.status.changed", { status });
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

	private emitSessionUpdated(): void {
		const previousModel = this.lastSessionModel;
		const previousName = this.lastSessionName;
		this.lastSessionModel = this.session.model;
		this.lastSessionName = this.session.name;
		this.emit("session.updated", { session: this.session });
		if (previousModel !== this.session.model) {
			this.emit("session.updated.model", {
				session: this.session,
				modelId: this.session.model,
			});
			void this.maybeHandleModelSwitchOverflow();
		}
	}

	private getRetrySettings() {
		return resolveRetrySettings(this.settings.retry);
	}

	private getRestoredThinkingLevel(
		model: Model<Api> | undefined,
	): ThinkingLevel {
		return clampThinkingLevel(this.session.thinkingLevel, model);
	}

	private async persistSessionThinkingLevel(
		level: ThinkingLevel,
	): Promise<void> {
		if (this.session.thinkingLevel === level) return;
		if (this.isEmpty()) {
			this.session = {
				...this.session,
				thinkingLevel: level,
				updatedAt: new Date().toISOString(),
			};
			this.emit("session.changed", { session: this.session });
			this.emitSessionUpdated();
			return;
		}
		try {
			this.session = await updateSession(this.session, {
				thinkingLevel: level,
			});
			this.emit("session.changed", { session: this.session });
			this.emitSessionUpdated();
		} catch (err) {
			this.emit("notification.error", {
				title: "Session save failed",
				lines: [String(err)],
			});
		}
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
		this.pendingCount = this.agent.getPendingFollowUps().length;
		this.emit("runtime.pending.changed", { count: this.pendingCount });
		this.emit("runtime.pending.messages.changed", {
			messages: this.agent.getPendingFollowUps(),
		});
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

	private isEmpty(): boolean {
		return this.session.turns.length === 0;
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
			this.emit("notification.error", {
				title: "Auto-compaction failed",
				lines: [`No API key available for ${model.provider}.`],
			});
			return;
		}

		this.isCompacting = true;
		this.emit("runtime.panel.changed", {
			panel: {
				pending: true,
				title: `Compacting session… (${contextUsage?.percent ?? 0}%)`,
			},
		});

		try {
			const result = await compactSessionTurns({
				session: this.session,
				model,
				apiKey,
			});
			if (!result) return;

			this.agent.replaceFromTurns(result.turns);
			this.session = await updateSession(this.session, {
				turns: result.turns,
				model: model.id,
				thinkingLevel: this.agent.state.thinkingLevel,
			});
			this.emit("notification.info", {
				title: "Session compacted",
				lines: [
					`Context reached ${contextUsage?.percent ?? 0}%; compacted ${result.compactedTurnCount} turns into 1 summary turn.`,
					`Kept ${result.keptTurnCount} recent turns unchanged.`,
				],
			});
		} catch (error) {
			this.emit("notification.error", {
				title: "Auto-compaction failed",
				lines: [error instanceof Error ? error.message : String(error)],
			});
		} finally {
			this.isCompacting = false;
		}
	}

	private async persistTurns(): Promise<boolean> {
		try {
			this.session = await updateSession(this.session, {
				turns: this.agent.turns,
				model: this.agent.state.model?.id,
				thinkingLevel: this.agent.state.thinkingLevel,
			});
			return true;
		} catch (err) {
			this.emit("notification.error", {
				title: "Session save failed",
				lines: [String(err)],
			});
			return false;
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
		this.emit("session.turns.changed", { turns: [...this.agent.turns] });
		this.emit("runtime.status.changed", { status: this.snapshotStatus() });
	}

	private scheduleContinue(): void {
		setTimeout(() => {
			void this.agent.continue().catch((error) => {
				this.emit("notification.error", {
					title: "Retry failed",
					lines: [error instanceof Error ? error.message : String(error)],
				});
				this.retryAttempt = 0;
				this.retryAbortController = null;
				this.overflowRecoveryAttempted = false;
				this.emit("runtime.panel.changed", {
					panel: { pending: false, title: "" },
				});
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
		this.emit("runtime.panel.changed", {
			panel: {
				pending: true,
				title: `Retrying (${this.retryAttempt}/${settings.maxRetries}) in ${Math.ceil(delayMs / 1000)}s…`,
			},
		});
		this.retryAbortController = new AbortController();
		try {
			await this.sleepWithAbort(delayMs, this.retryAbortController.signal);
		} catch {
			this.retryAttempt = 0;
			this.retryAbortController = null;
			this.emit("runtime.panel.changed", {
				panel: { pending: false, title: "" },
			});
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
			this.emit("notification.error", {
				title: "Context overflow recovery failed",
				lines: [
					"Kit already attempted one compact-and-retry recovery for this overflow.",
					message.errorMessage ??
						"Start a new session or switch to a larger-context model.",
				],
			});
			this.overflowRecoveryAttempted = false;
			this.resolveRecovery();
			return false;
		}

		const apiKey = await getApiKey(model.provider);
		if (!apiKey) return false;

		this.overflowRecoveryAttempted = true;
		this.removeTerminalAssistantErrorFromLiveState();
		this.emit("runtime.panel.changed", {
			panel: { pending: true, title: `Compacting session for retry…` },
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
			this.session = await updateSession(this.session, {
				turns: result.turns,
				model: model.id,
				thinkingLevel: this.agent.state.thinkingLevel,
			});
			this.emit("session.changed", { session: this.session });
			this.emitSessionUpdated();
			this.emit("session.turns.changed", { turns: [...this.agent.turns] });
			this.emit("runtime.status.changed", { status: this.snapshotStatus() });
			this.emit("notification.info", {
				title: "Session compacted",
				lines: [
					`Recovered from a context overflow by compacting ${result.compactedTurnCount} turns into 1 summary turn.`,
					`Kept ${result.keptTurnCount} recent turns unchanged.`,
				],
			});
			this.scheduleContinue();
			return true;
		} catch (error) {
			this.emit("notification.error", {
				title: "Context overflow recovery failed",
				lines: [error instanceof Error ? error.message : String(error)],
			});
			this.resolveRecovery();
			return false;
		}
	}

	private async finalizeAgentRun(messages: AgentMessage[]): Promise<void> {
		await this.persistTurns();
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
		this.emit("session.changed", { session: this.session });
		this.emitSessionUpdated();
		this.emit("session.turns.changed", { turns: [...this.agent.turns] });
		this.emit("runtime.status.changed", { status: this.snapshotStatus() });
		this.emit("runtime.panel.changed", {
			panel: { pending: false, title: "" },
		});
		const completedTurn = this.agent.turns.at(-1) ?? null;
		this.emit("turn.completed", {
			turn: completedTurn,
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
			this.emit("notification.error", {
				title: "Model too small for session",
				lines: [
					`No API key available for ${model.provider} to compact this session for ${model.name ?? model.id}.`,
					"Start a new session or hand off to continue with this model.",
				],
			});
			return;
		}

		this.overflowRecoveryInFlight = true;
		this.emit("runtime.panel.changed", {
			panel: {
				pending: true,
				title: `Adapting session to ${model.name ?? model.id}… (${contextUsage.percent}%)`,
			},
		});

		try {
			const result = await compactSessionTurns({
				session: this.session,
				model,
				apiKey,
			});
			if (!result) {
				this.emit("notification.error", {
					title: "Model too small for session",
					lines: [
						`${model.name ?? model.id} cannot fit this session, even after optimized compaction.`,
						"Start a new session or hand off to continue with this model.",
					],
				});
				return;
			}

			this.agent.replaceFromTurns(result.turns);
			this.session = await updateSession(this.session, {
				turns: result.turns,
				model: model.id,
				thinkingLevel: this.agent.state.thinkingLevel,
			});
			this.emit("session.changed", { session: this.session });
			this.emitSessionUpdated();
			this.emit("session.turns.changed", { turns: [...this.agent.turns] });
			this.emit("runtime.status.changed", { status: this.snapshotStatus() });

			const nextUsage = getRuntimeContextUsage(
				this.agent.state.messages,
				model,
			);
			if (nextUsage && nextUsage.percent > 100) {
				this.emit("notification.error", {
					title: "Model too small for session",
					lines: [
						`${model.name ?? model.id} is still over capacity after compaction (${nextUsage.percent}%).`,
						"Start a new session or hand off to continue with this model.",
					],
				});
			}
		} catch (error) {
			this.emit("notification.error", {
				title: "Model switch compaction failed",
				lines: [
					error instanceof Error ? error.message : String(error),
					"Start a new session or hand off to continue with this model.",
				],
			});
		} finally {
			this.overflowRecoveryInFlight = false;
			this.emit("runtime.panel.changed", {
				panel: { pending: false, title: "" },
			});
		}
	}

	private handleAgentEvent(event: AgentEvent) {
		this.createRecoveryPromiseForAgentEnd(event);
		switch (event.type) {
			case "agent_start":
				this.emit("runtime.panel.changed", {
					panel: { pending: true, title: "Working…" },
				});
				this.emit("session.turns.changed", { turns: [...this.agent.turns] });
				this.emit("runtime.status.changed", { status: this.snapshotStatus() });
				break;

			case "turn_start":
				this.syncPendingState();
				break;

			case "message_start":
				if (event.message.role === "assistant") {
					this.emit("runtime.panel.changed", {
						panel: { pending: true, title: "Thinking…" },
					});
				}
				break;

			case "message_update": {
				const ame = event.assistantMessageEvent;
				if (
					ame?.type === "thinking_delta" &&
					"delta" in ame &&
					typeof ame.delta === "string" &&
					ame.delta.trim()
				) {
					this.emit("runtime.panel.changed", {
						panel: {
							pending: true,
							title: ame.delta.replace(/\s+/g, " ").trim(),
						},
					});
				}
				break;
			}

			case "message_end":
				this.emit("session.turns.changed", { turns: [...this.agent.turns] });
				this.emit("runtime.status.changed", { status: this.snapshotStatus() });
				break;

			case "tool_execution_end":
				this.emit("tool.completed", {});
				break;

			case "agent_end":
				void this.finalizeAgentRun(event.messages).catch((error) => {
					this.retryAttempt = 0;
					this.overflowRecoveryAttempted = false;
					this.resolveRecovery();
					this.emit("notification.error", {
						title: "Session save failed",
						lines: [error instanceof Error ? error.message : String(error)],
					});
					this.emit("runtime.panel.changed", {
						panel: { pending: false, title: "" },
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
			const error = new Error(
				"Attachments not supported in queued follow-ups. Wait for the current turn to finish before sending attached reviews.",
			);
			this.emit("notification.error", {
				title: "Queued follow-up unsupported",
				lines: [error.message],
			});
			throw error;
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
		try {
			await this.agent.prompt(message as unknown as AgentMessage);
			await this.waitForRecovery();
		} catch (err) {
			this.emit("notification.error", {
				title: "Agent error",
				lines: [String(err)],
			});
			throw err;
		}
	}

	abort(): void {
		this.retryAbortController?.abort();
		this.agent.abort();
	}

	addTool(tool: AgentTool): void {
		this.extraTools.push(tool);
		this.agent.setTools([
			...createDefaultTools(this.session.cwd),
			...this.extraTools,
		]);
	}

	/**
	 * Append text to the effective system prompt and propagate it to the agent.
	 * Intended for plugins that own a feature-specific policy or tool guidelines
	 * that should be part of the system prompt without being baked into `App.tsx`.
	 */
	addSystemPromptAddition(text: string): void {
		const trimmed = text.trim();
		if (!trimmed) return;
		this.systemPromptAdditions.push(trimmed);
		this.agent.setSystemPrompt(this.getEffectiveSystemPrompt());
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
		this.agent.appendCustomMessage(pendingMessage);
		this.emit("session.turns.changed", { turns: [...this.agent.turns] });

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
		this.agent.replaceCustomMessage(
			(message) =>
				message.role === "bashExecution" &&
				"id" in message &&
				message.id === id,
			bashMessage,
		);
		this.emit("session.turns.changed", { turns: [...this.agent.turns] });
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
			this.emit("notification.info", { title: "Steering", lines: [] });
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
	setDebugSection(key: string, lines: string[]): void {
		this.debugSections.set(key, lines);
	}

	getDebugSections(): Map<string, string[]> {
		return this.debugSections;
	}

	getMessages(): AgentMessage[] {
		return this.agent.turns.flatMap((turn) => turn.messages);
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
		this.session = { ...this.session, thinkingLevel: restoredThinkingLevel };
		this.applySessionContext(this.session);
		this.syncPendingState();
		this.emit("session.changed", { session: this.session });
		this.emitSessionUpdated();
		this.emit("session.turns.changed", { turns: [] });
		this.emit("runtime.status.changed", { status: this.snapshotStatus() });
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

		await writeSession(child);
		this.session = child;
		this.agent.replaceFromTurns(child.turns);
		const restoredThinkingLevel = this.getRestoredThinkingLevel(
			this.agent.state.model,
		);
		this.agent.setThinkingLevel(restoredThinkingLevel);
		this.session = { ...this.session, thinkingLevel: restoredThinkingLevel };
		this.applySessionContext(child);
		this.syncPendingState();
		this.emit("session.changed", { session: this.session });
		this.emitSessionUpdated();
		this.emit("session.turns.changed", { turns: [...this.session.turns] });
		this.emit("runtime.status.changed", { status: this.snapshotStatus() });

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
		this.emit("session.changed", { session: this.session });
		this.emitSessionUpdated();
		this.emit("session.turns.changed", { turns: [...this.session.turns] });
		this.emit("runtime.status.changed", { status: this.snapshotStatus() });
		return true;
	}

	async mergeUp(): Promise<void> {
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

		this.showPanel("Merging child session into parent…");
		try {
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
			const saved = await this.persistTurns();
			if (!saved) {
				throw new Error(
					"Failed to save merged summary into the parent session.",
				);
			}
			this.emit("session.changed", { session: this.session });
			this.emitSessionUpdated();
			this.emit("session.turns.changed", { turns: [...this.session.turns] });
			this.emit("runtime.status.changed", { status: this.snapshotStatus() });

			await deleteSession(child.id);
			this.emit("notification.info", {
				title: "Session squashed",
				lines: [
					`Merged ${child.name?.trim() || child.id.slice(0, 8)} into ${this.session.name?.trim() || this.session.id.slice(0, 8)}.`,
				],
			});
		} finally {
			this.hidePanel();
		}
	}

	async reloadSession(): Promise<void> {
		const reloaded =
			(await findSessionById(this.session.id)) ??
			(await readSession(this.session.id));
		if (reloaded) {
			this.session = reloaded;
			this.agent.replaceFromTurns(this.session.turns);
			const model = this.findModelById(this.session.model);
			if (model) this.agent.setModel(model);
			const restoredThinkingLevel = this.getRestoredThinkingLevel(
				this.agent.state.model,
			);
			this.agent.setThinkingLevel(restoredThinkingLevel);
			this.session = { ...this.session, thinkingLevel: restoredThinkingLevel };
		}
		this.applySessionContext(this.session);
		this.syncPendingState();
		this.emit("session.changed", { session: this.session });
		this.emitSessionUpdated();
		this.emit("session.turns.changed", { turns: [...this.session.turns] });
		this.emit("runtime.status.changed", { status: this.snapshotStatus() });
		this.emit("notification.info", {
			title: "Session reloaded",
			lines: [
				"Reloaded session state, context files, tools, and runtime status.",
			],
		});
	}

	async setSessionName(name: string): Promise<void> {
	  this.session.name = name
		writeSession(this.session);
		this.emit("session.name.changed", { name });
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
		return getAuthenticatedProviders().flatMap((provider) =>
			getModels(provider as KnownProvider),
		);
	}

	getCurrentModelId(): string | undefined {
		return this.agent.state.model?.id;
	}

	getCurrentModel(): Model<Api> | undefined {
		return this.agent.state.model;
	}

	setModel(model: Model<Api>): void {
		this.agent.setModel(model);
		this.emit("runtime.status.changed", { status: this.snapshotStatus() });

		if (this.isEmpty()) {
			this.session = {
				...this.session,
				model: model.id,
				updatedAt: new Date().toISOString(),
			};
			this.emit("session.changed", { session: this.session });
			this.emitSessionUpdated();
			return;
		}

		void updateSession(this.session, { model: model.id })
			.then((updated) => {
				this.session = updated;
				this.emit("session.changed", { session: this.session });
				this.emitSessionUpdated();
			})
			.catch((err) => {
				this.emit("notification.error", {
					title: "Session save failed",
					lines: [String(err)],
				});
			});
	}

	setThinkingLevel(level: ThinkingLevel): void {
		const clamped = clampThinkingLevel(level, this.agent.state.model);
		this.agent.setThinkingLevel(clamped);
		this.session = {
			...this.session,
			thinkingLevel: clamped,
		};
		this.emit("runtime.status.changed", { status: this.snapshotStatus() });
		void this.persistSessionThinkingLevel(clamped);
	}

	showPanel(title: string): void {
		this.emit("runtime.panel.changed", {
			panel: { pending: true, title },
		});
	}

	hidePanel(): void {
		this.emit("runtime.panel.changed", {
			panel: { pending: false, title: "" },
		});
	}

	emitError(title: string, lines: string[]): void {
		this.emit("notification.error", { title, lines });
	}

	emitInfo(title: string, lines: string[]): void {
		this.emit("notification.info", { title, lines });
	}

	emitWarning(title: string, lines: string[]): void {
		this.emit("notification.warning", { title, lines });
	}

	emitSettingsChanged(settings: Settings): void {
		this.settings = settings;
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
		this.gitWatcher?.dispose();
		this.gitWatcher = null;
		this.listeners.clear();
		this.exactListeners.clear();
		this.prefixListeners.length = 0;
	}
}

function getAuthenticatedProviders(): string[] {
	const fromAuth = getAuthenticatedProviderIds();
	const fromEnv = getProviders().filter(
		(p) => !fromAuth.includes(p) && getEnvApiKey(p) != null,
	);
	return [...fromAuth, ...fromEnv];
}

function resolveDefaultModel(preferredModelId?: string): Model<Api> {
	const providers = getAuthenticatedProviders();

	if (providers.length === 0) {
		throw new Error("No authenticated providers found. Run /login first.");
	}

	if (preferredModelId) {
		for (const provider of providers) {
			for (const model of getModels(provider as KnownProvider)) {
				if (model.id === preferredModelId) return model;
			}
		}
	}

	for (const provider of providers) {
		const models = getModels(provider as KnownProvider);
		if (models[0]) return models[0];
	}

	throw new Error("No models available for authenticated providers.");
}
