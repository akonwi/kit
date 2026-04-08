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
	createSession,
	deleteSession,
	findSessionById,
	listAllSessions,
	listSessionsForCwd,
	openRecentSession,
	readSession,
	type Session,
	type SessionSummary,
	updateSession,
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

// Register built-in API providers (Anthropic, OpenAI, etc.) once
registerBuiltInApiProviders();

// --- Types ---

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
	| { type: "error"; title: string; lines: string[] }
	| { type: "info"; title: string; lines: string[] };

export type AgentRuntime = {
	// Messaging
	submitUserMessage(text: string): Promise<void>;
	abort(): void;
	sendFollowUp(text: string): void;
	sendSteer(text: string): void;
	clearPendingMessages(): void;
	drainPendingMessages(): string[];
	promotePendingFollowUpsToSteering(): void;
	getPendingMessageCount(): number;
	getPendingMessages(): string[];

	// Session management
	getSession(): Session;
	getMessages(): AgentMessage[];
	getTurns(): Turn[];
	newSession(cwd?: string): Promise<void>;
	switchSession(id: string): Promise<boolean>;
	setSessionName(name: string): Promise<void>;
	listAllSessions(): Promise<SessionSummary[]>;
	listSessionsForCwd(cwd: string): Promise<SessionSummary[]>;
	deleteSession(id: string): Promise<void>;

	// Model management
	getStatus(): RuntimeStatus;
	getAvailableModels(): Array<Model<Api>>;
	getCurrentModelId(): string | undefined;
	setModel(model: Model<Api>): void;
	setThinkingLevel(level: ThinkingLevel): void;

	// UI helpers
	showPanel(title: string): void;
	hidePanel(): void;
	emitError(title: string, lines: string[]): void;
	emitInfo(title: string, lines: string[]): void;
	onQuit(handler: () => void): void;
	quit(): void;

	// Lifecycle
	subscribe(listener: (event: AgentRuntimeEvent) => void): () => void;
	dispose(): void;
};

// --- Factory ---

export async function createAgentRuntime(
	initialSession: Session | null,
	options?: { extraTools?: AgentTool[] },
): Promise<AgentRuntime> {
	// Load or create session
	let session = initialSession ?? (await openRecentSession(process.cwd()));

	const defaultModel = resolveDefaultModel(session.model);
	console.log(
		"[runtime] model:",
		defaultModel.id,
		"provider:",
		defaultModel.provider,
		"api:",
		defaultModel.api,
	);

	// Create Agent
	const agent = KitAgent.fromSession(session, {
		initialState: {
			model: defaultModel,
			tools: [
				...createDefaultTools(session.cwd),
				...(options?.extraTools ?? []),
			],
		},
		getApiKey: (provider) => getApiKey(provider),
	});

	const listeners = new Set<(event: AgentRuntimeEvent) => void>();
	let quitHandler: (() => void) | null = null;
	let pendingCount = 0;
	let isCompacting = false;

	const emit = (event: AgentRuntimeEvent) => {
		for (const listener of listeners) listener(event);
	};

	const syncPendingState = () => {
		pendingCount = agent.getPendingFollowUps().length;
		emit({ type: "pending_changed", count: pendingCount });
		emit({
			type: "pending_messages_changed",
			messages: agent.getPendingFollowUps(),
		});
	};

	const snapshotStatus = (): RuntimeStatus => ({
		model: agent.state.model?.name ?? agent.state.model?.id ?? "no model",
		thinkingLevel: agent.state.thinkingLevel ?? "off",
		isStreaming: agent.state.isStreaming,
		git: getGitInfo(session.cwd),
		contextUsage: getRuntimeContextUsage(
			agent.state.messages,
			agent.state.model,
		),
	});

	const isEmpty = () => session.turns.length === 0;

	const maybeAutoCompact = async () => {
		if (isCompacting) return;
		const model = agent.state.model;
		const contextUsage = getRuntimeContextUsage(agent.state.messages, model);
		if (!model || !shouldAutoCompact(contextUsage?.percent)) return;
		const apiKey = await getApiKey(model.provider);
		if (!apiKey) {
			emit({
				type: "error",
				title: "Auto-compaction failed",
				lines: [`No API key available for ${model.provider}.`],
			});
			return;
		}

		isCompacting = true;
		emit({
			type: "panel",
			panel: {
				pending: true,
				title: `Compacting session… (${contextUsage?.percent ?? 0}%)`,
			},
		});

		try {
			const result = await compactSessionTurns({
				session,
				model,
				apiKey,
			});
			if (!result) return;

			agent.replaceFromTurns(result.turns);
			session = await updateSession(session, {
				turns: result.turns,
				model: model.id,
			});
			emit({
				type: "info",
				title: "Session compacted",
				lines: [
					`Context reached ${contextUsage?.percent ?? 0}%; compacted ${result.compactedTurnCount} turns into 1 summary turn.`,
					`Kept ${result.keptTurnCount} recent turns unchanged.`,
				],
			});
		} catch (error) {
			emit({
				type: "error",
				title: "Auto-compaction failed",
				lines: [error instanceof Error ? error.message : String(error)],
			});
		} finally {
			isCompacting = false;
		}
	};

	// Persist turns to disk after each turn
	const persistTurns = async () => {
		try {
			session = await updateSession(session, {
				turns: agent.turns,
				model: agent.state.model?.id,
			});
		} catch (err) {
			emit({
				type: "error",
				title: "Session save failed",
				lines: [String(err)],
			});
		}
	};

	// Subscribe to agent events
	const unsubscribe = agent.subscribe((event: AgentEvent) => {
		switch (event.type) {
			case "agent_start":
				emit({ type: "panel", panel: { pending: true, title: "Working…" } });
				emit({ type: "turns_changed", turns: [...agent.turns] });
				emit({ type: "status_changed", status: snapshotStatus() });
				break;

			case "turn_start":
				syncPendingState();
				break;

			case "message_start":
				if (event.message.role === "assistant") {
					emit({ type: "panel", panel: { pending: true, title: "Thinking…" } });
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
					emit({
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
				emit({ type: "turns_changed", turns: [...agent.turns] });
				emit({ type: "status_changed", status: snapshotStatus() });
				break;

			case "tool_execution_end":
				emit({ type: "tool_completed" });
				break;

			case "agent_end":
				void persistTurns()
					.then(async () => {
						await maybeAutoCompact();
						emit({ type: "session_changed", session });
						emit({ type: "turns_changed", turns: [...agent.turns] });
						emit({ type: "status_changed", status: snapshotStatus() });
						emit({ type: "panel", panel: { pending: false, title: "" } });
						emit({
							type: "turn_complete",
							turn: agent.turns.at(-1) ?? null,
						});
						syncPendingState();
					})
					.catch((error) => {
						emit({
							type: "error",
							title: "Session save failed",
							lines: [error instanceof Error ? error.message : String(error)],
						});
						emit({ type: "panel", panel: { pending: false, title: "" } });
					});
				break;
		}
	});

	return {
		// --- Messaging ---

		async submitUserMessage(text) {
			try {
				await agent.prompt(text);
			} catch (err) {
				emit({ type: "error", title: "Agent error", lines: [String(err)] });
			}
		},

		abort() {
			agent.abort();
		},

		sendFollowUp(text) {
			const msg: UserMessage = {
				role: "user",
				content: text,
				timestamp: Date.now(),
			};
			agent.followUp(msg);
			syncPendingState();
		},

		sendSteer(text) {
			const msg: UserMessage = {
				role: "user",
				content: text,
				timestamp: Date.now(),
			};
			agent.steer(msg);
		},

		clearPendingMessages() {
			agent.clearPendingFollowUps();
			syncPendingState();
		},

		drainPendingMessages() {
			const drained = agent.drainPendingFollowUps();
			syncPendingState();
			return drained;
		},

		promotePendingFollowUpsToSteering() {
			const drained = agent.drainPendingFollowUps();
			for (const text of drained) {
				const msg: UserMessage = {
					role: "user",
					content: text,
					timestamp: Date.now(),
				};
				agent.steer(msg);
			}
			syncPendingState();
			if (drained.length > 0) {
				emit({ type: "info", title: "Steering", lines: [] });
			}
		},

		getPendingMessageCount() {
			return agent.getPendingFollowUps().length;
		},

		getPendingMessages() {
			return agent.getPendingFollowUps();
		},

		// --- Session ---

		getSession() {
			return session;
		},

		getMessages() {
			return agent.turns.flatMap((turn) => turn.messages);
		},

		getTurns() {
			return [...agent.turns];
		},

		async newSession(cwd?: string) {
			const targetCwd = cwd ?? session.cwd;
			session = await createSession(targetCwd, agent.state.model?.id);
			agent.replaceFromTurns([]);
			agent.setTools(createDefaultTools(targetCwd));
			syncPendingState();
			emit({ type: "session_changed", session });
			emit({ type: "turns_changed", turns: [] });
			emit({ type: "status_changed", status: snapshotStatus() });
		},

		async switchSession(id) {
			const target = (await findSessionById(id)) ?? (await readSession(id));
			if (!target) return false;
			session = target;
			agent.replaceFromTurns(session.turns);
			agent.setTools(createDefaultTools(session.cwd));
			syncPendingState();
			emit({ type: "session_changed", session });
			emit({ type: "turns_changed", turns: [...session.turns] });
			emit({ type: "status_changed", status: snapshotStatus() });
			return true;
		},

		async setSessionName(name) {
			if (isEmpty()) {
				session = {
					...session,
					name,
					updatedAt: new Date().toISOString(),
				};
				emit({ type: "session_changed", session });
				return;
			}
			session = await updateSession(session, { name });
			emit({ type: "session_changed", session });
		},

		async listAllSessions() {
			return listAllSessions();
		},

		async listSessionsForCwd(cwd) {
			return listSessionsForCwd(cwd);
		},

		async deleteSession(id) {
			if (id === session.id)
				throw new Error("Cannot delete the active session");
			await deleteSession(id);
		},

		// --- Model ---

		getStatus() {
			return snapshotStatus();
		},

		getAvailableModels() {
			return getAuthenticatedProviders().flatMap((provider) =>
				getModels(provider as KnownProvider),
			);
		},

		getCurrentModelId() {
			return agent.state.model?.id;
		},

		setModel(model: Model<Api>) {
			agent.setModel(model);
			emit({ type: "status_changed", status: snapshotStatus() });

			if (isEmpty()) {
				session = {
					...session,
					model: model.id,
					updatedAt: new Date().toISOString(),
				};
				emit({ type: "session_changed", session });
				return;
			}

			void updateSession(session, { model: model.id })
				.then((updated) => {
					session = updated;
					emit({ type: "session_changed", session });
				})
				.catch((err) => {
					emit({
						type: "error",
						title: "Session save failed",
						lines: [String(err)],
					});
				});
		},

		setThinkingLevel(level) {
			agent.setThinkingLevel(level);
			emit({ type: "status_changed", status: snapshotStatus() });
		},

		// --- UI ---

		showPanel(title) {
			emit({ type: "panel", panel: { pending: true, title } });
		},

		hidePanel() {
			emit({ type: "panel", panel: { pending: false, title: "" } });
		},

		emitError(title, lines) {
			emit({ type: "error", title, lines });
		},

		emitInfo(title, lines) {
			emit({ type: "info", title, lines });
		},

		onQuit(handler) {
			quitHandler = handler;
		},

		quit() {
			quitHandler?.();
		},

		// --- Lifecycle ---

		subscribe(listener) {
			listeners.add(listener);
			return () => listeners.delete(listener);
		},

		dispose() {
			unsubscribe();
			listeners.clear();
		},
	};
}

// --- Helpers ---

function getAuthenticatedProviders(): string[] {
	// Auth.json keys are the source of truth for authenticated providers.
	// Also pick up any providers configured via env vars only.
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

	// Try to match the preferred model from the last session
	if (preferredModelId) {
		for (const provider of providers) {
			for (const m of getModels(provider as KnownProvider)) {
				if (m.id === preferredModelId) return m;
			}
		}
	}

	// First model from first authenticated provider
	for (const provider of providers) {
		const models = getModels(provider as KnownProvider);
		if (models[0]) return models[0];
	}

	throw new Error("No models available for authenticated providers.");
}
