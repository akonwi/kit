import { createSignal } from "solid-js";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { readScratchpad, writeScratchpad } from "./storage";

export type ScratchpadController = ReturnType<
	typeof createScratchpadController
>;

type ScratchpadStorage = {
	read: (sessionId: string) => string;
	write: (sessionId: string, content: string) => void;
};

const defaultStorage: ScratchpadStorage = {
	read: readScratchpad,
	write: writeScratchpad,
};

const AUTOSAVE_DELAY_MS = 250;

export function createScratchpadController(
	runtime: AgentRuntime,
	storage: ScratchpadStorage = defaultStorage,
) {
	const [content, setContentSignal] = createSignal(
		storage.read(runtime.getSession().id),
	);
	const [draft, setDraftSignal] = createSignal(content());
	const [editing, setEditing] = createSignal(false);
	const [dirty, setDirty] = createSignal(false);
	const [sessionId, setSessionId] = createSignal(runtime.getSession().id);
	const pendingDrafts = new Map<string, string>();
	let autosaveTimer: ReturnType<typeof setTimeout> | undefined;

	function applyContent(next: string): void {
		setContentSignal(next);
		runtime.setScratchpadContent(next);
	}

	function resetDraft(next: string): void {
		setDraftSignal(next);
		setEditing(false);
		setDirty(pendingDrafts.has(sessionId()));
	}

	function clearAutosaveTimer(): void {
		if (!autosaveTimer) return;
		clearTimeout(autosaveTimer);
		autosaveTimer = undefined;
	}

	function writeContent(targetSessionId: string, next: string): void {
		storage.write(targetSessionId, next);
	}

	function persistContent(targetSessionId: string, next: string): boolean {
		try {
			writeContent(targetSessionId, next);
			if (pendingDrafts.get(targetSessionId) === next) {
				pendingDrafts.delete(targetSessionId);
			}
			if (targetSessionId === sessionId())
				setDirty(pendingDrafts.has(targetSessionId));
			return true;
		} catch {
			pendingDrafts.set(targetSessionId, next);
			if (targetSessionId === sessionId()) setDirty(true);
			return false;
		}
	}

	function persistDraft(targetSessionId = sessionId()): boolean {
		return persistContent(targetSessionId, draft());
	}

	function flushAutosave(): boolean {
		clearAutosaveTimer();
		return persistDraft();
	}

	function scheduleAutosave(): void {
		clearAutosaveTimer();
		autosaveTimer = setTimeout(() => {
			autosaveTimer = undefined;
			persistDraft();
		}, AUTOSAVE_DELAY_MS);
	}

	applyContent(content());

	const unsubscribe = runtime.subscribe("session.active.changed", (event) => {
		const previousSessionId = sessionId();
		const previousContent = dirty()
			? (pendingDrafts.get(previousSessionId) ?? draft())
			: editing()
				? draft()
				: content();
		if (editing() || dirty() || autosaveTimer) {
			clearAutosaveTimer();
			persistContent(previousSessionId, previousContent);
		}
		const nextSession = event.session;
		let nextContent =
			pendingDrafts.get(nextSession.id) ?? storage.read(nextSession.id);
		if (
			nextSession.parentSessionId === previousSessionId &&
			nextContent.trim().length === 0 &&
			previousContent.trim().length > 0
		) {
			nextContent = previousContent;
			persistContent(nextSession.id, nextContent);
		}
		setSessionId(nextSession.id);
		applyContent(nextContent);
		resetDraft(nextContent);
	});

	return {
		content,
		draft,
		editing,
		dirty,
		sessionId,
		enterEdit(): void {
			setDraftSignal(content());
			runtime.setScratchpadContent(content());
			setEditing(true);
		},
		setDraft(next: string): void {
			setDraftSignal(next);
			pendingDrafts.set(sessionId(), next);
			setDirty(true);
			applyContent(next);
			scheduleAutosave();
		},
		cancelEdit(): void {
			clearAutosaveTimer();
			pendingDrafts.delete(sessionId());
			setDirty(false);
			resetDraft(content());
			runtime.setScratchpadContent(content());
		},
		autosaveDraft(): boolean {
			const ok = flushAutosave();
			applyContent(draft());
			if (ok) setEditing(false);
			return ok;
		},
		flushAutosave,
		saveDraft(): void {
			const next = draft();
			clearAutosaveTimer();
			writeContent(sessionId(), next);
			pendingDrafts.delete(sessionId());
			setDirty(false);
			applyContent(next);
			setEditing(false);
		},
		save(next: string): void {
			setDraftSignal(next);
			clearAutosaveTimer();
			writeContent(sessionId(), next);
			pendingDrafts.delete(sessionId());
			setDirty(false);
			applyContent(next);
			setEditing(false);
		},
		reload(): void {
			clearAutosaveTimer();
			pendingDrafts.delete(sessionId());
			setDirty(false);
			const next = storage.read(sessionId());
			applyContent(next);
			resetDraft(next);
		},
		dispose(): void {
			clearAutosaveTimer();
			if (editing() || dirty()) persistDraft();
			for (const [pendingSessionId, pendingContent] of pendingDrafts) {
				persistContent(pendingSessionId, pendingContent);
			}
			unsubscribe();
		},
	};
}
