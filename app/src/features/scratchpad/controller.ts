import { createSignal } from "solid-js";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import {
	ensureScratchpad,
	mutateScratchpad,
	readScratchpad,
	readScratchpadFile,
	type ScratchpadMutationResult,
	scratchpadPath,
	writeScratchpad,
} from "./storage";

export type ScratchpadController = ReturnType<
	typeof createScratchpadController
>;

type ScratchpadStorage = {
	read: (sessionId: string) => string;
	readForTool?: (sessionId: string) => string;
	write: (sessionId: string, content: string) => void;
	ensure?: (sessionId: string) => void;
	mutate?: (
		sessionId: string,
		update: (current: string) => string | null,
	) => ScratchpadMutationResult;
};

const defaultStorage: ScratchpadStorage = {
	read: readScratchpad,
	readForTool: readScratchpadFile,
	write: writeScratchpad,
	ensure: ensureScratchpad,
	mutate: mutateScratchpad,
};

const AUTOSAVE_DELAY_MS = 250;

export function createScratchpadController(
	runtime: AgentRuntime,
	storage: ScratchpadStorage = defaultStorage,
) {
	const initialSessionId = runtime.getSession().id;
	storage.ensure?.(initialSessionId);
	const initialContent = storage.read(initialSessionId);
	const [content, setContentSignal] = createSignal(initialContent);
	const [draft, setDraftSignal] = createSignal(content());
	const [editing, setEditing] = createSignal(false);
	const [dirty, setDirty] = createSignal(false);
	const [sessionId, setSessionId] = createSignal(runtime.getSession().id);
	const pendingDrafts = new Map<string, string>();
	const persistedContents = new Map([[initialSessionId, initialContent]]);
	const listeners = new Set<
		(snapshot: { sessionId: string; content: string }) => void
	>();
	let publishedSessionId = initialSessionId;
	let autosaveTimer: ReturnType<typeof setTimeout> | undefined;

	function applyContent(next: string): void {
		const nextSessionId = sessionId();
		const changed = content() !== next || publishedSessionId !== nextSessionId;
		setContentSignal(next);
		runtime.setScratchpadContent(next);
		if (!changed) return;
		publishedSessionId = nextSessionId;
		for (const listener of listeners) {
			try {
				listener({ sessionId: nextSessionId, content: next });
			} catch {
				// Scratchpad persistence must not fail because an observer failed.
			}
		}
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

	function persistContent(targetSessionId: string, next: string): boolean {
		try {
			const expected =
				persistedContents.get(targetSessionId) ?? storage.read(targetSessionId);
			const result = storage.mutate
				? storage.mutate(targetSessionId, (current) =>
						current === expected ? next : null,
					)
				: (() => {
						const current = storage.read(targetSessionId);
						if (current !== expected && current !== next) {
							return { updated: false, content: current };
						}
						if (current !== next) storage.write(targetSessionId, next);
						return { updated: current !== next, content: next };
					})();
			if (!result.updated && result.content !== next) {
				pendingDrafts.set(targetSessionId, next);
				if (targetSessionId === sessionId()) setDirty(true);
				return false;
			}
			persistedContents.set(targetSessionId, result.content);
			if (pendingDrafts.get(targetSessionId) === next) {
				pendingDrafts.delete(targetSessionId);
			}
			if (targetSessionId === sessionId()) {
				setDirty(pendingDrafts.has(targetSessionId));
			}
			return true;
		} catch {
			pendingDrafts.set(targetSessionId, next);
			if (targetSessionId === sessionId()) setDirty(true);
			return false;
		}
	}

	function persistDraft(targetSessionId = sessionId()): boolean {
		if (!pendingDrafts.has(targetSessionId)) return true;
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

	function reloadContent(): void {
		clearAutosaveTimer();
		pendingDrafts.delete(sessionId());
		setDirty(false);
		const next = storage.read(sessionId());
		persistedContents.set(sessionId(), next);
		applyContent(next);
		resetDraft(next);
	}

	function applyAtomicUpdate(
		targetSessionId: string,
		update: (persisted: string) => string | null,
	): ScratchpadMutationResult | null {
		if (targetSessionId !== sessionId()) return null;
		const result = storage.mutate
			? storage.mutate(targetSessionId, update)
			: (() => {
					const persisted = storage.read(targetSessionId);
					const next = update(persisted);
					if (next === null || next === persisted) {
						return { updated: false, content: persisted };
					}
					storage.write(targetSessionId, next);
					return { updated: true, content: next };
				})();
		persistedContents.set(targetSessionId, result.content);
		if (!result.updated) {
			if (!dirty() && result.content !== content()) {
				setDraftSignal(result.content);
				applyContent(result.content);
				setEditing(false);
			}
			return result;
		}
		setDraftSignal(result.content);
		clearAutosaveTimer();
		pendingDrafts.delete(targetSessionId);
		setDirty(false);
		applyContent(result.content);
		setEditing(false);
		return result;
	}

	function flushForFileOperation(): void {
		if ((dirty() || autosaveTimer) && !flushAutosave()) {
			throw new Error("Could not save pending scratchpad edits.");
		}
	}

	function syncFileContent(next: string): void {
		pendingDrafts.delete(sessionId());
		setDirty(false);
		setDraftSignal(next);
		persistedContents.set(sessionId(), next);
		applyContent(next);
	}

	applyContent(content());
	const unregisterFileOperations = runtime.registerFileOperationHandler({
		handles: (filePath) => filePath === scratchpadPath(sessionId()),
		read: () => {
			flushForFileOperation();
			const next = (storage.readForTool ?? storage.read)(sessionId());
			syncFileContent(next);
			return next;
		},
		write: (_filePath, next) => {
			flushForFileOperation();
			if (!applyAtomicUpdate(sessionId(), () => next)) {
				throw new Error("The active scratchpad changed.");
			}
		},
		mutate: (_filePath, update) => {
			flushForFileOperation();
			const result = applyAtomicUpdate(sessionId(), update);
			if (!result) throw new Error("The active scratchpad changed.");
			return result;
		},
	});

	const unsubscribeSession = runtime.subscribe(
		"session.active.changed",
		(event) => {
			const previousSessionId = sessionId();
			const previousContent = dirty()
				? (pendingDrafts.get(previousSessionId) ?? draft())
				: storage.read(previousSessionId);
			if (dirty() || autosaveTimer) {
				clearAutosaveTimer();
				persistContent(previousSessionId, previousContent);
			}
			const nextSession = event.session;
			storage.ensure?.(nextSession.id);
			const persistedNextContent = storage.read(nextSession.id);
			if (!pendingDrafts.has(nextSession.id)) {
				persistedContents.set(nextSession.id, persistedNextContent);
			}
			let nextContent =
				pendingDrafts.get(nextSession.id) ?? persistedNextContent;
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
		},
	);

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
			const persisted =
				persistedContents.get(sessionId()) ?? storage.read(sessionId());
			applyContent(persisted);
			resetDraft(persisted);
		},
		autosaveDraft(): boolean {
			const ok = flushAutosave();
			applyContent(draft());
			if (ok) setEditing(false);
			return ok;
		},
		flushAutosave,
		applyAtomicUpdate,
		reload: reloadContent,
		subscribe(
			listener: (snapshot: { sessionId: string; content: string }) => void,
		): () => void {
			listeners.add(listener);
			return () => listeners.delete(listener);
		},
		dispose(): void {
			clearAutosaveTimer();
			if (dirty()) persistDraft();
			for (const [pendingSessionId, pendingContent] of pendingDrafts) {
				persistContent(pendingSessionId, pendingContent);
			}
			unsubscribeSession();
			unregisterFileOperations();
			listeners.clear();
		},
	};
}
