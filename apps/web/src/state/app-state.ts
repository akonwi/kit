import { homedir } from "node:os";
import { createStore } from "solid-js/store";
import { createFileIndex, type FileIndex } from "../features/files";
import { createThreadIndex, type ThreadIndex } from "../features/threads";
import { safeProcessCwd } from "../process-cwd";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { Session } from "../session";
import { type Toast, type ToastInput, toastForRuntimeRecord } from "./toasts";

export type SessionMeta = {
	id: string;
	name: string | undefined;
	cwd: string;
	hasSession: boolean;
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
		cwd: formatCwd(safeProcessCwd()),
		hasSession: false,
	};
}

// ── App state factory ──────────────────────────────────────────────

export function createAppState(runtime: AgentRuntime | null) {
	const [state, setState] = createStore<AppState>({
		toasts: [],
		pendingMessages: runtime ? runtime.getPendingMessages() : [],
		sessionMeta: buildSessionMeta(runtime?.getSession() ?? null),
		debugEntry: null,
	});

	const fileIndex: FileIndex = createFileIndex(
		runtime?.getSession().cwd ?? safeProcessCwd(),
	);
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

	function showToast(toast: ToastInput) {
		const id = nextToastId++;
		const legacyLines = (toast as ToastInput & { lines?: string[] }).lines;
		const subtitle =
			toast.subtitle ??
			legacyLines?.filter((line) => line.length > 0).join(" · ");
		setState("toasts", (prev) => [
			...prev,
			{
				id,
				title: toast.title,
				subtitle: subtitle || undefined,
				variant: toast.variant,
				persistent: toast.persistent,
			},
		]);
		if (!toast.persistent) {
			toastTimers.set(
				id,
				setTimeout(() => dismissToast(id), 10_000),
			);
		}
	}

	// ── Runtime subscription ───────────────────────────────────────

	const FILE_INDEX_INVALIDATE_INTERVAL = 5;
	let toolCompletionCount = 0;

	const unsubscribeRuntime = runtime?.subscribe((event) => {
		switch (event.type) {
			case "session.active.changed":
				setState("sessionMeta", buildSessionMeta(event.session));
				fileIndex.setCwd(event.session.cwd);
				break;
			case "agent.tool.ended":
				toolCompletionCount++;
				if (toolCompletionCount >= FILE_INDEX_INVALIDATE_INTERVAL) {
					toolCompletionCount = 0;
					fileIndex.invalidate();
				}
				break;
			case "chat.message-queue.changed":
				setState("pendingMessages", event.messages);
				break;
		}
		const toast = toastForRuntimeRecord(event);
		if (toast) showToast(toast);
	});

	// ── Debug ─────────────────────────────────────────────────────

	function dispose() {
		unsubscribeRuntime?.();
		for (const timer of toastTimers.values()) {
			clearTimeout(timer);
		}
		toastTimers.clear();
	}

	return {
		state,
		fileIndex,
		threadIndex,
		dismissToast,
		showToast,
		dispose,
	};
}
