import { homedir } from "node:os";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { createStore } from "solid-js/store";
import type { AgentRuntime, RuntimeStatus } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import { createFileIndex, type FileIndex } from "../features/files";
import { createThreadIndex, type ThreadIndex } from "../features/threads";

export type PanelState = {
	pending: boolean;
	title: string;
};

export type FooterStatusState = {
	cwd: string;
	model: string;
	thinkingLevel: string;
	contextPct: string;
};

export type SessionMeta = {
	sessionId: string;
	sessionName: string | undefined;
	sessionCwd: string;
	hasSession: boolean;
};

export type AppState = {
	messages: AgentMessage[];
	panel: PanelState;
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
		return {
			model: status.model,
			thinkingLevel: status.thinkingLevel,
			contextPct: status.contextPct,
		};
	}
	return { model: "no-model", thinkingLevel: "off", contextPct: "–" };
}

function applyRuntimeStatus(
	current: FooterStatusState,
	status: RuntimeStatus,
): FooterStatusState {
	return {
		...current,
		model: status.model,
		thinkingLevel: status.thinkingLevel,
		contextPct: status.contextPct,
	};
}

function buildSessionMeta(session: LoadedSession | null): SessionMeta {
	if (session) {
		return {
			sessionId: session.sessionId,
			sessionName: session.sessionName,
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
	session: LoadedSession | null,
	runtime: AgentRuntime | null,
) {
	const messages = runtime ? runtime.getMessages() : [];
	const footer = deriveFooterStatus(runtime);

	const [state, setState] = createStore<AppState>({
		messages,
		panel: { pending: false, title: "" },
		footerStatus: { cwd: formatCwd(process.cwd()), ...footer },
		sessionMeta: buildSessionMeta(session),
		debugEntry: null,
	});

	const fileIndex: FileIndex = createFileIndex(process.cwd());
	const threadIndex: ThreadIndex | null = runtime ? createThreadIndex(runtime) : null;

	// ── Runtime subscription ───────────────────────────────────────

	const FILE_INDEX_INVALIDATE_INTERVAL = 5;
	let toolCompletionCount = 0;

	runtime?.subscribe((event) => {
		switch (event.type) {
			case "messages_changed":
				setState("messages", event.messages);
				break;
			case "status_changed":
				setState(
					"footerStatus",
					applyRuntimeStatus(state.footerStatus, event.status),
				);
				break;
			case "session_changed":
				setState("sessionMeta", buildSessionMeta(event.session));
				threadIndex?.invalidate();
				break;
			case "panel":
				setState("panel", event.panel);
				break;
			case "tool_completed":
				toolCompletionCount++;
				if (toolCompletionCount >= FILE_INDEX_INVALIDATE_INTERVAL) {
					toolCompletionCount = 0;
					fileIndex.invalidate();
				}
				break;
			case "error":
				console.error(event);
				break;
		}
	});

	// ── Debug ─────────────────────────────────────────────────────

	return {
		state,
		fileIndex,
		threadIndex,
	};
}
