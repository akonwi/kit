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
	type Usage,
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
import { type GitInfo, getGitInfo } from "./git-info";
import { KitAgent } from "./kit-agent";

// Register built-in API providers (Anthropic, OpenAI, etc.) once
registerBuiltInApiProviders();

// --- Types ---

export type RuntimeContextUsage = {
	tokens: number;
	contextWindow: number;
	percent: number;
	usageTokens: number;
	trailingTokens: number;
	lastUsageIndex: number | null;
};

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
	| { type: "error"; title: string; lines: string[] }
	| { type: "info"; title: string; lines: string[] };

export type AgentRuntime = {
	// Messaging
	submitUserMessage(text: string): Promise<void>;
	abort(): void;
	sendFollowUp(text: string): void;
	sendSteer(text: string): void;
	clearPendingMessages(): void;
	getPendingMessageCount(): number;

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

	const emit = (event: AgentRuntimeEvent) => {
		for (const listener of listeners) listener(event);
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
				void persistTurns().then(() => {
					emit({ type: "session_changed", session });
					emit({ type: "turns_changed", turns: [...agent.turns] });
					emit({ type: "status_changed", status: snapshotStatus() });
					emit({ type: "panel", panel: { pending: false, title: "" } });
					emit({
						type: "turn_complete",
						turn: agent.turns.at(-1) ?? null,
					});
					pendingCount = agent.hasQueuedMessages() ? 1 : 0;
					emit({ type: "pending_changed", count: pendingCount });
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
			pendingCount = agent.hasQueuedMessages() ? 1 : 0;
			emit({ type: "pending_changed", count: pendingCount });
		},

		sendSteer(text) {
			const msg: UserMessage = {
				role: "user",
				content: text,
				timestamp: Date.now(),
			};
			agent.steer(msg);
			pendingCount = agent.hasQueuedMessages() ? 1 : 0;
			emit({ type: "pending_changed", count: pendingCount });
		},

		clearPendingMessages() {
			agent.clearAllQueues();
			pendingCount = 0;
			emit({ type: "pending_changed", count: 0 });
		},

		getPendingMessageCount() {
			return pendingCount;
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

function calculateContextTokens(usage: Usage): number {
	return (
		usage.totalTokens ||
		usage.input + usage.output + usage.cacheRead + usage.cacheWrite
	);
}

function getAssistantUsage(message: AgentMessage): Usage | undefined {
	if (message.role !== "assistant") return undefined;
	if (message.stopReason === "aborted" || message.stopReason === "error") {
		return undefined;
	}
	return message.usage;
}

function estimateTokens(message: AgentMessage): number {
	let chars = 0;

	switch (message.role) {
		case "user": {
			if (typeof message.content === "string") {
				chars = message.content.length;
			} else {
				for (const block of message.content) {
					if (block.type === "text") chars += block.text.length;
					if (block.type === "image") chars += 4800;
				}
			}
			break;
		}

		case "assistant": {
			for (const block of message.content) {
				if (block.type === "text") chars += block.text.length;
				if (block.type === "thinking") chars += block.thinking.length;
				if (block.type === "toolCall") {
					chars += block.name.length + JSON.stringify(block.arguments).length;
				}
			}
			break;
		}

		case "toolResult": {
			for (const block of message.content) {
				if (block.type === "text") chars += block.text.length;
				if (block.type === "image") chars += 4800;
			}
			break;
		}

		default: {
			if ("content" in message) {
				const content = message.content;
				if (typeof content === "string") {
					chars = content.length;
				} else if (Array.isArray(content)) {
					for (const block of content) {
						if (
							typeof block === "object" &&
							block !== null &&
							"type" in block &&
							block.type === "text" &&
							"text" in block &&
							typeof block.text === "string"
						) {
							chars += block.text.length;
						}
					}
				}
			}
		}
	}

	return Math.ceil(chars / 4);
}

function getRuntimeContextUsage(
	messages: AgentMessage[],
	model: Model<Api> | undefined,
): RuntimeContextUsage | null {
	if (!model?.contextWindow) return null;

	for (let index = messages.length - 1; index >= 0; index--) {
		const usage = getAssistantUsage(messages[index]);
		if (!usage) continue;
		const usageTokens = calculateContextTokens(usage);
		let trailingTokens = 0;
		for (let i = index + 1; i < messages.length; i++) {
			trailingTokens += estimateTokens(messages[i]);
		}
		const tokens = usageTokens + trailingTokens;
		return {
			tokens,
			contextWindow: model.contextWindow,
			percent: Math.round((tokens / model.contextWindow) * 100),
			usageTokens,
			trailingTokens,
			lastUsageIndex: index,
		};
	}

	let tokens = 0;
	for (const message of messages) {
		tokens += estimateTokens(message);
	}

	return {
		tokens,
		contextWindow: model.contextWindow,
		percent: Math.round((tokens / model.contextWindow) * 100),
		usageTokens: 0,
		trailingTokens: tokens,
		lastUsageIndex: null,
	};
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
