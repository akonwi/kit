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
import type { Settings } from "../settings";
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

registerBuiltInApiProviders();

const DEFAULT_SYSTEM_PROMPT = `You are kit, a coding assistant running in the terminal.
You have access to tools to read and modify files, run commands, search code, and more.
Be concise and direct. Prefer surgical edits over full rewrites when practical.`;

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

export type AgentRuntimeEvent =
	| { type: "turns_changed"; turns: Turn[] }
	| { type: "status_changed"; status: RuntimeStatus }
	| { type: "session_changed"; session: Session }
	| { type: "session_updated"; session: Session }
	| { type: "panel"; panel: RuntimePanelState }
	| { type: "tool_completed" }
	| { type: "turn_complete"; turn: Turn | null }
	| { type: "pending_changed"; count: number }
	| { type: "pending_messages_changed"; messages: string[] }
	| { type: "settings_changed"; settings: Settings }
	| { type: "error"; title: string; lines: string[] }
	| { type: "info"; title: string; lines: string[] };

export class AgentRuntime {
	private session: Session;
	private agent: KitAgent;
	// biome-ignore lint/suspicious/noExplicitAny: heterogeneous tool collection, matches pi-core convention
	private extraTools: AgentTool<any>[];
	private systemPromptAdditions: string[];
	private listeners = new Set<(event: AgentRuntimeEvent) => void>();
	private quitHandler: (() => void) | null = null;
	private pendingCount = 0;
	private isCompacting = false;
	private unsubscribeAgent: (() => void) | null = null;
	private contextFiles: ContextFile[] = [];
	private debugSections = new Map<string, string[]>();
	private gitWatcher: GitInfoWatcher | null = null;
	private gitInfo: GitInfo = { branch: null, dirty: false };
	private lastSessionModel: string | undefined;
	private overflowRecoveryInFlight = false;
	private lastOverflowRecoveryKey: string | null = null;

	constructor(
		session: Session,
		options?: {
			extraTools?: AgentTool<any>[]; // biome-ignore lint/suspicious/noExplicitAny: heterogeneous tool collection
			systemPromptAdditions?: string[];
		},
	) {
		this.session = session;
		this.lastSessionModel = session.model;
		this.extraTools = options?.extraTools ?? [];
		this.systemPromptAdditions = options?.systemPromptAdditions ?? [];
		const defaultModel = resolveDefaultModel(session.model);
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
				systemPrompt: this.getEffectiveSystemPrompt(),
				tools: [...createDefaultTools(session.cwd), ...this.extraTools],
			},
			getApiKey: (provider) => getApiKey(provider),
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
			this.emit({ type: "status_changed", status: this.snapshotStatus() });
		});
		this.gitInfo = this.gitWatcher.getCurrent();
	}

	private emit(event: AgentRuntimeEvent) {
		for (const listener of this.listeners) listener(event);
	}

	private emitSessionUpdated(): void {
		const previousModel = this.lastSessionModel;
		this.lastSessionModel = this.session.model;
		this.emit({ type: "session_updated", session: this.session });
		if (previousModel !== this.session.model) {
			void this.maybeHandleModelSwitchOverflow();
		}
	}

	private syncPendingState() {
		this.pendingCount = this.agent.getPendingFollowUps().length;
		this.emit({ type: "pending_changed", count: this.pendingCount });
		this.emit({
			type: "pending_messages_changed",
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
			this.emit({
				type: "error",
				title: "Auto-compaction failed",
				lines: [`No API key available for ${model.provider}.`],
			});
			return;
		}

		this.isCompacting = true;
		this.emit({
			type: "panel",
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
			});
			this.emit({
				type: "info",
				title: "Session compacted",
				lines: [
					`Context reached ${contextUsage?.percent ?? 0}%; compacted ${result.compactedTurnCount} turns into 1 summary turn.`,
					`Kept ${result.keptTurnCount} recent turns unchanged.`,
				],
			});
		} catch (error) {
			this.emit({
				type: "error",
				title: "Auto-compaction failed",
				lines: [error instanceof Error ? error.message : String(error)],
			});
		} finally {
			this.isCompacting = false;
		}
	}

	private async persistTurns() {
		try {
			this.session = await updateSession(this.session, {
				turns: this.agent.turns,
				model: this.agent.state.model?.id,
			});
		} catch (err) {
			this.emit({
				type: "error",
				title: "Session save failed",
				lines: [String(err)],
			});
		}
	}

	private async maybeHandleModelSwitchOverflow(): Promise<void> {
		const model = this.agent.state.model;
		if (!model || this.isCompacting || this.overflowRecoveryInFlight) return;

		const contextUsage = getRuntimeContextUsage(this.agent.state.messages, model);
		if (!contextUsage || contextUsage.percent <= 100) return;

		const recoveryKey = `${this.session.id}:${model.id}:${this.session.updatedAt}:${this.agent.turns.length}`;
		if (this.lastOverflowRecoveryKey === recoveryKey) return;
		this.lastOverflowRecoveryKey = recoveryKey;

		const apiKey = await getApiKey(model.provider);
		if (!apiKey) {
			this.emit({
				type: "error",
				title: "Model too small for session",
				lines: [
					`No API key available for ${model.provider} to compact this session for ${model.name ?? model.id}.`,
					"Start a new session or hand off to continue with this model.",
				],
			});
			return;
		}

		this.overflowRecoveryInFlight = true;
		this.emit({
			type: "panel",
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
				this.emit({
					type: "error",
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
			});
			this.emit({ type: "session_changed", session: this.session });
			this.emitSessionUpdated();
			this.emit({ type: "turns_changed", turns: [...this.agent.turns] });
			this.emit({ type: "status_changed", status: this.snapshotStatus() });

			const nextUsage = getRuntimeContextUsage(this.agent.state.messages, model);
			if (nextUsage && nextUsage.percent > 100) {
				this.emit({
					type: "error",
					title: "Model too small for session",
					lines: [
						`${model.name ?? model.id} is still over capacity after compaction (${nextUsage.percent}%).`,
						"Start a new session or hand off to continue with this model.",
					],
				});
			}
		} catch (error) {
			this.emit({
				type: "error",
				title: "Model switch compaction failed",
				lines: [
					error instanceof Error ? error.message : String(error),
					"Start a new session or hand off to continue with this model.",
				],
			});
		} finally {
			this.overflowRecoveryInFlight = false;
			this.emit({ type: "panel", panel: { pending: false, title: "" } });
		}
	}

	private handleAgentEvent(event: AgentEvent) {
		switch (event.type) {
			case "agent_start":
				this.emit({
					type: "panel",
					panel: { pending: true, title: "Working…" },
				});
				this.emit({ type: "turns_changed", turns: [...this.agent.turns] });
				this.emit({ type: "status_changed", status: this.snapshotStatus() });
				break;

			case "turn_start":
				this.syncPendingState();
				break;

			case "message_start":
				if (event.message.role === "assistant") {
					this.emit({
						type: "panel",
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
					this.emit({
						type: "panel",
						panel: {
							pending: true,
							title: ame.delta.replace(/\s+/g, " ").trim(),
						},
					});
				}
				break;
			}

			case "message_end":
				this.emit({ type: "turns_changed", turns: [...this.agent.turns] });
				this.emit({ type: "status_changed", status: this.snapshotStatus() });
				break;

			case "tool_execution_end":
				this.emit({ type: "tool_completed" });
				break;

			case "agent_end":
				void this.persistTurns()
					.then(async () => {
						await this.maybeAutoCompact();
						this.emit({ type: "session_changed", session: this.session });
						this.emitSessionUpdated();
						this.emit({ type: "turns_changed", turns: [...this.agent.turns] });
						this.emit({
							type: "status_changed",
							status: this.snapshotStatus(),
						});
						this.emit({ type: "panel", panel: { pending: false, title: "" } });
						const completedTurn = this.agent.turns.at(-1) ?? null;
						this.emit({
							type: "turn_complete",
							turn: completedTurn,
						});
						this.syncPendingState();
					})
					.catch((error) => {
						this.emit({
							type: "error",
							title: "Session save failed",
							lines: [error instanceof Error ? error.message : String(error)],
						});
						this.emit({ type: "panel", panel: { pending: false, title: "" } });
					});
				break;
		}
	}

	async submitUserMessage(text: string): Promise<void> {
		try {
			await this.agent.prompt(text);
		} catch (err) {
			this.emit({ type: "error", title: "Agent error", lines: [String(err)] });
		}
	}

	abort(): void {
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
		const result = await runBash(command, this.session.cwd);

		const bashMessage: AgentMessage = {
			role: "bashExecution",
			command,
			output: result.output,
			exitCode: result.exitCode,
			cancelled: false,
			truncated: false,
			excludeFromContext,
			timestamp: Date.now(),
		};
		this.agent.appendCustomMessage(bashMessage);
		this.emit({ type: "turns_changed", turns: [...this.agent.turns] });
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
			this.emit({ type: "info", title: "Steering", lines: [] });
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
		this.session = await createSession(targetCwd, this.agent.state.model?.id);
		this.agent.replaceFromTurns([]);
		this.applySessionContext(this.session);
		this.syncPendingState();
		this.emit({ type: "session_changed", session: this.session });
		this.emitSessionUpdated();
		this.emit({ type: "turns_changed", turns: [] });
		this.emit({ type: "status_changed", status: this.snapshotStatus() });
	}

	async handoffSession(firstMessage?: string): Promise<Session> {
		if (this.session.turns.length === 0) {
			throw new Error("Nothing to hand off yet.");
		}

		const parentName = this.session.name?.trim() || "Untitled";
		const now = new Date().toISOString();
		const child: Session = {
			...(await createSession(this.session.cwd, this.agent.state.model?.id)),
			parentSessionId: this.session.id,
			name: `handoff: ${parentName}`,
			model: this.agent.state.model?.id ?? this.session.model,
			createdAt: now,
			updatedAt: now,
			turns: structuredClone(this.session.turns),
		};

		await writeSession(child);
		this.session = child;
		this.agent.replaceFromTurns(child.turns);
		this.applySessionContext(child);
		this.syncPendingState();
		this.emit({ type: "session_changed", session: this.session });
		this.emitSessionUpdated();
		this.emit({ type: "turns_changed", turns: [...this.session.turns] });
		this.emit({ type: "status_changed", status: this.snapshotStatus() });

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
		this.applySessionContext(this.session);
		this.syncPendingState();
		this.emit({ type: "session_changed", session: this.session });
		this.emitSessionUpdated();
		this.emit({ type: "turns_changed", turns: [...this.session.turns] });
		this.emit({ type: "status_changed", status: this.snapshotStatus() });
		return true;
	}

	async reloadSession(): Promise<void> {
		const reloaded =
			(await findSessionById(this.session.id)) ?? (await readSession(this.session.id));
		if (reloaded) {
			this.session = reloaded;
			this.agent.replaceFromTurns(this.session.turns);
			const model = this.findModelById(this.session.model);
			if (model) this.agent.setModel(model);
		}
		this.applySessionContext(this.session);
		this.syncPendingState();
		this.emit({ type: "session_changed", session: this.session });
		this.emitSessionUpdated();
		this.emit({ type: "turns_changed", turns: [...this.session.turns] });
		this.emit({ type: "status_changed", status: this.snapshotStatus() });
		this.emit({
			type: "info",
			title: "Session reloaded",
			lines: ["Reloaded session state, context files, tools, and runtime status."],
		});
	}

	async setSessionName(name: string): Promise<void> {
		if (this.isEmpty()) {
			this.session = {
				...this.session,
				name,
				updatedAt: new Date().toISOString(),
			};
			this.emit({ type: "session_changed", session: this.session });
			this.emitSessionUpdated();
			return;
		}
		this.session = await updateSession(this.session, { name });
		this.emit({ type: "session_changed", session: this.session });
		this.emitSessionUpdated();
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
		this.emit({ type: "status_changed", status: this.snapshotStatus() });

		if (this.isEmpty()) {
			this.session = {
				...this.session,
				model: model.id,
				updatedAt: new Date().toISOString(),
			};
			this.emit({ type: "session_changed", session: this.session });
			this.emitSessionUpdated();
			return;
		}

		void updateSession(this.session, { model: model.id })
			.then((updated) => {
				this.session = updated;
				this.emit({ type: "session_changed", session: this.session });
				this.emitSessionUpdated();
			})
			.catch((err) => {
				this.emit({
					type: "error",
					title: "Session save failed",
					lines: [String(err)],
				});
			});
	}

	setThinkingLevel(level: ThinkingLevel): void {
		this.agent.setThinkingLevel(level);
		this.emit({ type: "status_changed", status: this.snapshotStatus() });
	}

	showPanel(title: string): void {
		this.emit({ type: "panel", panel: { pending: true, title } });
	}

	hidePanel(): void {
		this.emit({ type: "panel", panel: { pending: false, title: "" } });
	}

	emitError(title: string, lines: string[]): void {
		this.emit({ type: "error", title, lines });
	}

	emitInfo(title: string, lines: string[]): void {
		this.emit({ type: "info", title, lines });
	}

	emitSettingsChanged(settings: Settings): void {
		this.emit({ type: "settings_changed", settings });
	}

	onQuit(handler: () => void): void {
		this.quitHandler = handler;
	}

	quit(): void {
		this.quitHandler?.();
	}

	subscribe(listener: (event: AgentRuntimeEvent) => void): () => void {
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	dispose(): void {
		this.unsubscribeAgent?.();
		this.unsubscribeAgent = null;
		this.gitWatcher?.dispose();
		this.gitWatcher = null;
		this.listeners.clear();
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
