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
	loadNotificationConfigSync,
	type NotificationConfig,
	saveNotificationConfig,
} from "../features/notification-config";
import { ringBell, speak } from "../features/notifications";
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
import { createDefaultTools } from "../tools";
import { compactSessionTurns, shouldAutoCompact } from "./compaction";
import {
	getRuntimeContextUsage,
	type RuntimeContextUsage,
} from "./context-usage";
import { type GitInfo, getGitInfo } from "./git-info";
import { KitAgent } from "./kit-agent";

registerBuiltInApiProviders();

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
	| { type: "panel"; panel: RuntimePanelState }
	| { type: "tool_completed" }
	| { type: "turn_complete"; turn: Turn | null }
	| { type: "pending_changed"; count: number }
	| { type: "pending_messages_changed"; messages: string[] }
	| { type: "notification_config_changed"; config: NotificationConfig }
	| { type: "error"; title: string; lines: string[] }
	| { type: "info"; title: string; lines: string[] };

export class AgentRuntime {
	private session: Session;
	private agent: KitAgent;
	private listeners = new Set<(event: AgentRuntimeEvent) => void>();
	private quitHandler: (() => void) | null = null;
	private pendingCount = 0;
	private isCompacting = false;
	private unsubscribeAgent: (() => void) | null = null;
	private notificationConfig: NotificationConfig;

	constructor(session: Session, options?: { extraTools?: AgentTool[] }) {
		this.session = session;
		this.notificationConfig = loadNotificationConfigSync();
		const defaultModel = resolveDefaultModel(session.model);

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
				tools: [
					...createDefaultTools(session.cwd),
					...(options?.extraTools ?? []),
				],
			},
			getApiKey: (provider) => getApiKey(provider),
		});
		this.unsubscribeAgent = this.agent.subscribe((event) =>
			this.handleAgentEvent(event),
		);
	}

	private emit(event: AgentRuntimeEvent) {
		for (const listener of this.listeners) listener(event);
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
			git: getGitInfo(this.session.cwd),
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
		if (this.isCompacting) return;
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

	private notifyTurnComplete(turn: Turn | null): void {
		if (!turn) return;
		const isError = turn.messages.some(
			(message) =>
				message.role === "assistant" && message.stopReason === "error",
		);
		ringBell(isError, this.notificationConfig.bells.enabled);

		if (!this.notificationConfig.speech.enabled) return;
		const assistantText = getLastAssistantText(turn.messages);
		if (!assistantText) return;
		speak(assistantText, this.session.id, {
			maxChars: this.notificationConfig.speech.maxChars,
			voice: this.notificationConfig.speech.voice ?? undefined,
		});
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
						this.notifyTurnComplete(completedTurn);
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
		this.agent.setTools(createDefaultTools(targetCwd));
		this.syncPendingState();
		this.emit({ type: "session_changed", session: this.session });
		this.emit({ type: "turns_changed", turns: [] });
		this.emit({ type: "status_changed", status: this.snapshotStatus() });
	}

	async handoffSession(): Promise<Session> {
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
		this.agent.setTools(createDefaultTools(child.cwd));
		this.syncPendingState();
		this.emit({ type: "session_changed", session: this.session });
		this.emit({ type: "turns_changed", turns: [...this.session.turns] });
		this.emit({ type: "status_changed", status: this.snapshotStatus() });
		return child;
	}

	async switchSession(id: string): Promise<boolean> {
		const target = (await findSessionById(id)) ?? (await readSession(id));
		if (!target) return false;
		this.session = target;
		this.agent.replaceFromTurns(this.session.turns);
		this.agent.setTools(createDefaultTools(this.session.cwd));
		this.syncPendingState();
		this.emit({ type: "session_changed", session: this.session });
		this.emit({ type: "turns_changed", turns: [...this.session.turns] });
		this.emit({ type: "status_changed", status: this.snapshotStatus() });
		return true;
	}

	async setSessionName(name: string): Promise<void> {
		if (this.isEmpty()) {
			this.session = {
				...this.session,
				name,
				updatedAt: new Date().toISOString(),
			};
			this.emit({ type: "session_changed", session: this.session });
			return;
		}
		this.session = await updateSession(this.session, { name });
		this.emit({ type: "session_changed", session: this.session });
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

	getNotificationConfig(): NotificationConfig {
		return {
			bells: { ...this.notificationConfig.bells },
			speech: { ...this.notificationConfig.speech },
		};
	}

	async toggleBells(): Promise<boolean> {
		this.notificationConfig = {
			...this.notificationConfig,
			bells: { enabled: !this.notificationConfig.bells.enabled },
		};
		await saveNotificationConfig(this.notificationConfig);
		this.emit({
			type: "notification_config_changed",
			config: this.getNotificationConfig(),
		});
		return this.notificationConfig.bells.enabled;
	}

	async toggleSpeech(): Promise<boolean> {
		this.notificationConfig = {
			...this.notificationConfig,
			speech: {
				...this.notificationConfig.speech,
				enabled: !this.notificationConfig.speech.enabled,
			},
		};
		await saveNotificationConfig(this.notificationConfig);
		this.emit({
			type: "notification_config_changed",
			config: this.getNotificationConfig(),
		});
		return this.notificationConfig.speech.enabled;
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
			return;
		}

		void updateSession(this.session, { model: model.id })
			.then((updated) => {
				this.session = updated;
				this.emit({ type: "session_changed", session: this.session });
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
		this.listeners.clear();
	}
}

function getLastAssistantText(messages: AgentMessage[]): string {
	for (let i = messages.length - 1; i >= 0; i--) {
		const message = messages[i];
		if (message.role !== "assistant") continue;
		const text = message.content
			.filter((part) => part.type === "text")
			.map((part) => part.text)
			.join("\n")
			.trim();
		if (text) return text;
	}
	return "";
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
