import { homedir } from "node:os";
import { createStore } from "solid-js/store";
import { createFileIndex, type FileIndex } from "../features/files";
import { createThreadIndex, type ThreadIndex } from "../features/threads";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { Session } from "../session";

export type SessionMeta = {
	id: string;
	name: string | undefined;
	cwd: string;
	hasSession: boolean;
};

export type Toast = {
	id: number;
	variant: "error" | "warning" | "info";
	title: string;
	lines: string[];
};

export type AppState = {
	toasts: Toast[];
	pendingMessages: string[];
	sessionMeta: SessionMeta;
	debugEntry: string | null;
};

// ── Helpers ────────────────────────────────────────────────────────

function formatCwd(rawCwd: string): string {
	const home = homedir();
	return rawCwd.startsWith(home) ? `~${rawCwd.slice(home.length)}` : rawCwd;
}

function buildSessionMeta(session: Session | null): SessionMeta {
	if (session) {
		return {
			id: session.id,
			name: session.name,
			cwd: formatCwd(session.cwd),
			hasSession: true,
		};
	}
	return {
		id: "",
		name: undefined,
		cwd: formatCwd(process.cwd()),
		hasSession: false,
	};
}

// ── App state factory ──────────────────────────────────────────────

export function createAppState(
	session: Session | null,
	runtime: AgentRuntime | null,
) {
	const [state, setState] = createStore<AppState>({
		toasts: [],
		pendingMessages: runtime ? runtime.getPendingMessages() : [],
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
			case "session.active.changed":
				setState("sessionMeta", buildSessionMeta(event.session));
				break;
			case "session.changed":
			case "session.updated":
				setState("sessionMeta", buildSessionMeta(event.session as Session));
				threadIndex?.invalidate();
				break;
			case "session.name.changed":
				setState("sessionMeta", (meta) => ({
					...meta,
					name: event.name,
				}));
				break;
			case "agent.tool.ended":
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
