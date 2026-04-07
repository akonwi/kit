import type {
	AgentEvent,
	AgentMessage,
	AgentTool,
	ThinkingLevel,
} from "@mariozechner/pi-agent-core";
import { Agent } from "@mariozechner/pi-agent-core";
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
import { createDefaultTools } from "../tools";
import { type GitInfo, getGitInfo } from "./git-info";

// Register built-in API providers (Anthropic, OpenAI, etc.) once
registerBuiltInApiProviders();

// --- Types ---

export type RuntimeStatus = {
	model: string;
	thinkingLevel: string;
	isStreaming: boolean;
	git: GitInfo;
};

export type RuntimePanelState = {
	pending: boolean;
	title: string;
};

export type AgentRuntimeEvent =
	| { type: "messages_changed"; messages: AgentMessage[] }
	| { type: "status_changed"; status: RuntimeStatus }
	| { type: "session_changed"; session: Session }
	| { type: "panel"; panel: RuntimePanelState }
	| { type: "tool_completed" }
	| { type: "turn_complete"; messages: AgentMessage[] }
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

// --- Default system prompt ---

const DEFAULT_SYSTEM_PROMPT = `You are kit, a coding assistant running in the terminal.
You have access to tools to read and modify files, run commands, search code, and more.
Be concise and direct. Prefer surgical edits over full rewrites when practical.`;

// --- Factory ---

export async function createAgentRuntime(
	initialSession: Session | null,
	options?: { extraTools?: AgentTool[] },
): Promise<AgentRuntime> {
	// Load or create session
	let session = initialSession ?? (await openRecentSession(process.cwd()));

	// Pick default model — prefer claude-sonnet
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
	const agent = new Agent({
		initialState: {
			systemPrompt: DEFAULT_SYSTEM_PROMPT,
			model: defaultModel,
			thinkingLevel: "medium",
			messages: session.messages,
			tools: [
				...createDefaultTools(session.cwd),
				...(options?.extraTools ?? []),
			],
		},
		steeringMode: "all",
		followUpMode: "all",
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
	});

	// Persist messages to disk after each turn
	const persistMessages = async () => {
		try {
			session = await updateSession(session, {
				messages: agent.state.messages,
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
				emit({ type: "messages_changed", messages: [...agent.state.messages] });
				break;

			case "message_start":
				if (event.message.role === "assistant") {
					emit({ type: "panel", panel: { pending: true, title: "Thinking…" } });
				}
				break;

			case "message_update": {
				const ame = event.assistantMessageEvent;
				if (ame?.type === "thinking_delta" && (ame as any).delta?.trim()) {
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
				emit({ type: "messages_changed", messages: [...agent.state.messages] });
				emit({ type: "status_changed", status: snapshotStatus() });
				break;

			case "tool_execution_end":
				emit({ type: "tool_completed" });
				break;

			case "agent_end":
				void persistMessages().then(() => {
					emit({ type: "session_changed", session });
					emit({
						type: "messages_changed",
						messages: [...agent.state.messages],
					});
					emit({ type: "status_changed", status: snapshotStatus() });
					emit({ type: "panel", panel: { pending: false, title: "" } });
					emit({ type: "turn_complete", messages: [...agent.state.messages] });
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
			return [...agent.state.messages];
		},

		async newSession(cwd?: string) {
			const targetCwd = cwd ?? session.cwd;
			session = await createSession(targetCwd, agent.state.model?.id);
			agent.replaceMessages([]);
			agent.setTools(createDefaultTools(targetCwd));
			emit({ type: "session_changed", session });
			emit({ type: "messages_changed", messages: [] });
		},

		async switchSession(id) {
			const target = (await findSessionById(id)) ?? (await readSession(id));
			if (!target) return false;
			session = target;
			agent.replaceMessages(session.messages);
			agent.setTools(createDefaultTools(session.cwd));
			emit({ type: "session_changed", session });
			emit({ type: "messages_changed", messages: [...session.messages] });
			emit({ type: "status_changed", status: snapshotStatus() });
			return true;
		},

		async setSessionName(name) {
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
			return getProviders().flatMap((provider) => getModels(provider));
		},

		getCurrentModelId() {
			return agent.state.model?.id;
		},

		setModel(model: Model<Api>) {
			agent.setModel(model);
			emit({ type: "status_changed", status: snapshotStatus() });
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
