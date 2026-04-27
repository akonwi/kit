import { homedir } from "node:os";
import { createStore } from "solid-js/store";
import { createFileIndex, type FileIndex } from "../features/files";
import { createThreadIndex, type ThreadIndex } from "../features/threads";
import type { AgentRuntime, RuntimeStatus } from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { Turn } from "../session/types";
import type { LoadedSettings, Settings } from "../settings";

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
	variant: "error" | "warning" | "info";
	title: string;
	lines: string[];
};

export type AppState = {
	turns: Turn[];
	toasts: Toast[];
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

function resolveBellsEnabled(settings: Settings): boolean {
	return settings.bells ?? true;
}

function resolveSpeechEnabled(settings: Settings): boolean {
	const s = settings.speech;
	if (typeof s === "boolean") return s;
	if (s && typeof s === "object") return s.enabled ?? true;
	return true;
}

function deriveFooterStatus(
	runtime: AgentRuntime | null,
	settings: Settings,
): Omit<FooterStatusState, "cwd"> {
	if (runtime) {
		const status = runtime.getStatus();
		return {
			model: status.model,
			thinkingLevel: status.thinkingLevel,
			contextPct: status.contextUsage ? `${status.contextUsage.percent}%` : "–",
			gitBranch: status.git.branch,
			gitDirty: status.git.dirty,
			bellsEnabled: resolveBellsEnabled(settings),
			speechEnabled: resolveSpeechEnabled(settings),
			pendingMessages: runtime.getPendingMessageCount(),
		};
	}
	return {
		model: "no-model",
		thinkingLevel: "off",
		contextPct: "–",
		gitBranch: null,
		gitDirty: false,
		bellsEnabled: resolveBellsEnabled(settings),
		speechEnabled: resolveSpeechEnabled(settings),
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
	loaded: LoadedSettings,
	session: Session | null,
	runtime: AgentRuntime | null,
) {
	const turns = runtime ? runtime.getTurns() : [];
	const footer = deriveFooterStatus(runtime, loaded.settings);

	const [state, setState] = createStore<AppState>({
		turns,
		toasts: [],
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
			case "session.turns.changed":
				setState("turns", event.turns);
				for (const timer of toastTimers.values()) clearTimeout(timer);
				toastTimers.clear();
				setState("toasts", []);
				break;
			case "runtime.status.changed":
				setState(
					"footerStatus",
					applyRuntimeStatus(state.footerStatus, event.status),
				);
				break;
			case "session.changed":
			case "session.updated":
				setState("sessionMeta", buildSessionMeta(event.session as Session));
				threadIndex?.invalidate();
				break;
			case "session.name.changed":
				setState("sessionMeta", (meta) => ({
					...meta,
					sessionName: event.name,
				}));
				break;
			case "runtime.pending.changed":
				setState("footerStatus", "pendingMessages", event.count);
				break;
			case "settings.changed":
				setState(
					"footerStatus",
					"bellsEnabled",
					resolveBellsEnabled(event.settings),
				);
				setState(
					"footerStatus",
					"speechEnabled",
					resolveSpeechEnabled(event.settings),
				);
				break;
			case "runtime.pending.messages.changed":
				setState("pendingMessages", event.messages);
				break;
			case "tool.completed":
				toolCompletionCount++;
				if (toolCompletionCount >= FILE_INDEX_INVALIDATE_INTERVAL) {
					toolCompletionCount = 0;
					fileIndex.invalidate();
				}
				break;
			case "notification.error":
			case "notification.warning":
			case "notification.info":
				showToast({
					variant:
						event.type === "notification.error"
							? "error"
							: event.type === "notification.warning"
								? "warning"
								: "info",
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
