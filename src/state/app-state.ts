import { homedir } from "node:os";
import { createStore } from "solid-js/store";
import { createFileIndex, type FileIndex } from "../features/files";
import { createThreadIndex, type ThreadIndex } from "../features/threads";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { Session } from "../session";
import type { Toast, ToastInput } from "./toasts";

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
		cwd: formatCwd(process.cwd()),
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

	function showToast(toast: ToastInput) {
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
			case "agent.tool.ended":
				toolCompletionCount++;
				if (toolCompletionCount >= FILE_INDEX_INVALIDATE_INTERVAL) {
					toolCompletionCount = 0;
					fileIndex.invalidate();
				}
				break;
			case "chat.followups.promoted":
				showToast({
					title: "Steering",
					lines:
						event.count === 1
							? ["Promoted 1 queued follow-up into steering."]
							: [`Promoted ${event.count} queued follow-ups into steering.`],
					variant: "info",
				});
				break;
			case "session.compaction.completed.auto":
				showToast({
					title: "Session compacted",
					lines: [
						`Context reached ${event.contextPercent}%; compacted ${event.compactedTurnCount} turns into 1 summary turn.`,
						`Kept ${event.keptTurnCount} recent turns unchanged.`,
					],
					variant: "info",
				});
				break;
			case "session.compaction.failed.auto":
				showToast({
					title: "Auto-compaction failed",
					lines: [event.error],
					variant: "error",
				});
				break;
			case "session.compaction.completed.recovery":
				showToast({
					title: "Session compacted",
					lines: [
						`Recovered from a context overflow by compacting ${event.compactedTurnCount} turns into 1 summary turn.`,
						`Kept ${event.keptTurnCount} recent turns unchanged.`,
					],
					variant: "info",
				});
				break;
			case "session.compaction.failed.recovery":
				showToast({
					title: "Context overflow recovery failed",
					lines: [event.error],
					variant: "error",
				});
				break;
			case "session.compaction.failed.adaptation":
				showToast({
					title:
						event.cause === "compaction-error"
							? "Model switch compaction failed"
							: "Model too small for session",
					lines:
						event.cause === "compaction-error"
							? [
									event.error,
									"Start a new session or hand off to continue with this model.",
								]
							: [
									event.error,
									"Start a new session or hand off to continue with this model.",
								],
					variant: "error",
				});
				break;
			case "agent.retry.failed":
				if (event.error === "Retry cancelled before continue.") break;
				showToast({
					title: "Retry failed",
					lines: [event.error],
					variant: "error",
				});
				break;
			case "agent.run.failed":
				showToast({
					title: "Agent run failed",
					lines: [event.error],
					variant: "error",
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
