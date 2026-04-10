import { homedir } from "node:os";
import { createStore } from "solid-js/store";
import { createFileIndex, type FileIndex } from "../features/files";
import { createThreadIndex, type ThreadIndex } from "../features/threads";
import type { AgentRuntime, RuntimeStatus } from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { Turn } from "../session/types";
import type { LoadedSettings } from "../settings";

export type PanelState = {
	pending: boolean;
	title: string;
};

export type FooterStatusState = {
	cwd: string;
	model: string;
	thinkingLevel: string;
	contextPct: string;
	gitBranch: string | null;
	gitDirty: boolean;
	bellsEnabled: boolean;
	speechEnabled: boolean;
	pendingMessages: number;
};

export type SessionMeta = {
	sessionId: string;
	sessionName: string | undefined;
	sessionCwd: string;
	hasSession: boolean;
};

export type Toast = {
	id: number;
	variant: "error" | "info";
	title: string;
	lines: string[];
};

export type AppState = {
	turns: Turn[];
	toasts: Toast[];
	panel: PanelState;
	pendingMessages: string[];
	footerStatus: FooterStatusState;
	sessionMeta: SessionMeta;
	debugEntry: string | null;
};

// ── Helpers ────────────────────────────────────────────────────────

function formatCwd(rawCwd: string): string {
	const home = homedir();
	return rawCwd.startsWith(home) ? `~${rawCwd.slice(home.length)}` : rawCwd;
}

function deriveFooterStatus(
	runtime: AgentRuntime | null,
): Omit<FooterStatusState, "cwd"> {
	if (runtime) {
		const status = runtime.getStatus();
		const notificationConfig = runtime.getNotificationConfig();
		return {
			model: status.model,
			thinkingLevel: status.thinkingLevel,
			contextPct: status.contextUsage ? `${status.contextUsage.percent}%` : "–",
			gitBranch: status.git.branch,
			gitDirty: status.git.dirty,
			bellsEnabled: notificationConfig.bells.enabled,
			speechEnabled: notificationConfig.speech.enabled,
			pendingMessages: runtime.getPendingMessageCount(),
		};
	}
	return {
		model: "no-model",
		thinkingLevel: "off",
		contextPct: "–",
		gitBranch: null,
		gitDirty: false,
		bellsEnabled: true,
		speechEnabled: true,
		pendingMessages: 0,
	};
}

function applyRuntimeStatus(
	current: FooterStatusState,
	status: RuntimeStatus,
): FooterStatusState {
	return {
		...current,
		cwd: formatCwd(process.cwd()),
		model: status.model,
		thinkingLevel: status.thinkingLevel,
		contextPct: status.contextUsage ? `${status.contextUsage.percent}%` : "–",
		gitBranch: status.git.branch,
		gitDirty: status.git.dirty,
		pendingMessages: current.pendingMessages,
	};
}

function buildSessionMeta(session: Session | null): SessionMeta {
	if (session) {
		return {
			sessionId: session.id,
			sessionName: session.name,
			sessionCwd: session.cwd,
			hasSession: true,
		};
	}
	return {
		sessionId: "",
		sessionName: undefined,
		sessionCwd: process.cwd(),
		hasSession: false,
	};
}

// ── App state factory ──────────────────────────────────────────────

export function createAppState(
	_settings: LoadedSettings,
	session: Session | null,
	runtime: AgentRuntime | null,
) {
	const turns = runtime ? runtime.getTurns() : [];
	const footer = deriveFooterStatus(runtime);

	const [state, setState] = createStore<AppState>({
		turns,
		toasts: [],
		panel: { pending: false, title: "" },
		pendingMessages: runtime ? runtime.getPendingMessages() : [],
		footerStatus: { cwd: formatCwd(process.cwd()), ...footer },
		sessionMeta: buildSessionMeta(session),
		debugEntry: null,
	});

	const fileIndex: FileIndex = createFileIndex(process.cwd());
	const threadIndex: ThreadIndex | null = runtime
		? createThreadIndex(runtime)
		: null;

	// ── Toast ────────────────────────────────────────────────

	let nextToastId = 0;
	const toastTimers = new Map<number, ReturnType<typeof setTimeout>>();

	function dismissToast(id: number) {
		const timer = toastTimers.get(id);
		if (timer) {
			clearTimeout(timer);
			toastTimers.delete(id);
		}
		setState("toasts", (prev) => prev.filter((t) => t.id !== id));
	}

	function showToast(toast: Omit<Toast, "id">) {
		const id = nextToastId++;
		setState("toasts", (prev) => [...prev, { ...toast, id }]);
		toastTimers.set(
			id,
			setTimeout(() => dismissToast(id), 10_000),
		);
	}

	// ── Runtime subscription ───────────────────────────────────────

	const FILE_INDEX_INVALIDATE_INTERVAL = 5;
	let toolCompletionCount = 0;

	runtime?.subscribe((event) => {
		switch (event.type) {
			case "turns_changed":
				setState("turns", event.turns);
				for (const timer of toastTimers.values()) clearTimeout(timer);
				toastTimers.clear();
				setState("toasts", []);
				break;
			case "status_changed":
				setState(
					"footerStatus",
					applyRuntimeStatus(state.footerStatus, event.status),
				);
				break;
			case "session_changed":
				setState("sessionMeta", buildSessionMeta(event.session as Session));
				threadIndex?.invalidate();
				break;
			case "panel":
				setState("panel", event.panel);
				break;
			case "pending_changed":
				setState("footerStatus", "pendingMessages", event.count);
				break;
			case "pending_messages_changed":
				setState("pendingMessages", event.messages);
				break;
			case "notification_config_changed":
				setState("footerStatus", "bellsEnabled", event.config.bells.enabled);
				setState("footerStatus", "speechEnabled", event.config.speech.enabled);
				break;
			case "tool_completed":
				toolCompletionCount++;
				if (toolCompletionCount >= FILE_INDEX_INVALIDATE_INTERVAL) {
					toolCompletionCount = 0;
					fileIndex.invalidate();
				}
				break;
			case "error":
			case "info":
				showToast({
					variant: event.type,
					title: event.title,
					lines: event.lines,
				});
				break;
		}
	});

	// ── Debug ─────────────────────────────────────────────────────

	return {
		state,
		fileIndex,
		threadIndex,
		dismissToast,
		showToast,
	};
}
