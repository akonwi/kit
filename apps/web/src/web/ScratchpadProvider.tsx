/** @jsxImportSource solid-js */

import { isRecord } from "@akonwi/kit-session-client";
import {
	type Accessor,
	createContext,
	createEffect,
	createMemo,
	createSignal,
	type JSX,
	onCleanup,
	onMount,
	useContext,
} from "solid-js";
import { useWebClient } from "./WebClientContext";

const AUTOSAVE_DELAY_MS = 250;
const CONFLICT_ERROR = "Scratchpad changed elsewhere; reload before editing";

type PendingDraft = {
	draft: string;
	persistedContent: string;
	conflicted: boolean;
	error: string;
};

type ScratchpadContextValue = {
	open: Accessor<boolean>;
	draft: Accessor<string>;
	dirty: Accessor<boolean>;
	loading: Accessor<boolean>;
	saving: Accessor<boolean>;
	error: Accessor<string>;
	disabled: Accessor<boolean>;
	openScratchpad(): boolean;
	toggle(): void;
	close(): void;
	setDraft(content: string): void;
	reload(): void;
};

const ScratchpadContext = createContext<ScratchpadContextValue>();

export function useScratchpad(): ScratchpadContextValue {
	const value = useContext(ScratchpadContext);
	if (!value) throw new Error("ScratchpadProvider is missing");
	return value;
}

export function ScratchpadProvider(props: {
	children: JSX.Element;
}): JSX.Element {
	const { controller, snapshot } = useWebClient();
	const [open, setOpen] = createSignal(false);
	const [sessionId, setSessionId] = createSignal<string | null>(null);
	const [persistedContent, setPersistedContent] = createSignal("");
	const [draft, setDraftSignal] = createSignal("");
	const [dirty, setDirty] = createSignal(false);
	const [loading, setLoading] = createSignal(false);
	const [saving, setSaving] = createSignal(false);
	const [error, setError] = createSignal("");
	const [conflicted, setConflicted] = createSignal(false);
	const protocol = createMemo(() => snapshot().protocol);
	const disabled = createMemo(() => protocol().phase !== "live");
	const pendingDrafts = new Map<string, PendingDraft>();
	let loadGeneration = 0;
	let loadTargetSessionId: string | null = null;
	let autosaveTimer: ReturnType<typeof setTimeout> | undefined;
	let saveQueued = false;
	let saveGeneration = 0;
	let savingContent: string | null = null;
	let reconciliationPending = false;
	let observedPhase = protocol().phase;
	let observedActiveSessionId = activeSessionId();

	function activeSessionId(): string | null {
		const value = protocol().serverState.sessionId;
		return typeof value === "string" ? value : null;
	}

	function clearAutosave(): void {
		if (autosaveTimer === undefined) return;
		clearTimeout(autosaveTimer);
		autosaveTimer = undefined;
	}

	function rememberDraft(): void {
		const currentSessionId = sessionId();
		if (!currentSessionId) return;
		if (!dirty()) {
			pendingDrafts.delete(currentSessionId);
			return;
		}
		pendingDrafts.set(currentSessionId, {
			draft: draft(),
			persistedContent: persistedContent(),
			conflicted: conflicted(),
			error: error(),
		});
	}

	function scheduleAutosave(): void {
		clearAutosave();
		if (conflicted()) return;
		autosaveTimer = setTimeout(() => {
			autosaveTimer = undefined;
			void save();
		}, AUTOSAVE_DELAY_MS);
	}

	async function load(discardDraft: boolean): Promise<void> {
		if (disabled()) return;
		const generation = ++loadGeneration;
		const streamId = protocol().streamId;
		const expectedSessionId = activeSessionId();
		if (!expectedSessionId) return;
		loadTargetSessionId = expectedSessionId;
		const preserveDraft =
			!discardDraft && dirty() && sessionId() === expectedSessionId;
		setLoading(true);
		if (discardDraft) {
			setError("");
			setConflicted(false);
		}
		try {
			const next = await controller.getScratchpad();
			if (
				generation !== loadGeneration ||
				protocol().phase !== "live" ||
				protocol().streamId !== streamId ||
				activeSessionId() !== expectedSessionId ||
				next.sessionId !== expectedSessionId
			) {
				return;
			}
			setSessionId(next.sessionId);
			if (preserveDraft) {
				if (next.content === draft()) {
					setPersistedContent(next.content);
					setDirty(false);
					setError("");
					setConflicted(false);
					pendingDrafts.delete(next.sessionId);
					reconciliationPending = false;
					return;
				}
				if (next.content !== persistedContent()) {
					reconciliationPending = false;
					setConflicted(true);
					setError(CONFLICT_ERROR);
					rememberDraft();
					return;
				}
				setError("");
				setConflicted(false);
				reconciliationPending = false;
				rememberDraft();
				scheduleAutosave();
				return;
			}
			setPersistedContent(next.content);
			setDraftSignal(next.content);
			setDirty(false);
			setError("");
			setConflicted(false);
			reconciliationPending = false;
			pendingDrafts.delete(next.sessionId);
		} catch (cause) {
			if (generation !== loadGeneration) return;
			setError(cause instanceof Error ? cause.message : String(cause));
			rememberDraft();
		} finally {
			if (generation === loadGeneration) {
				loadTargetSessionId = null;
				setLoading(false);
			}
		}
	}

	async function save(): Promise<void> {
		if (!dirty() || conflicted() || error() || disabled()) return;
		if (saving()) {
			saveQueued = true;
			return;
		}
		const targetSessionId = sessionId();
		if (!targetSessionId) return;
		const expected = persistedContent();
		const next = draft();
		if (next === expected) {
			setDirty(false);
			pendingDrafts.delete(targetSessionId);
			return;
		}
		const generation = ++saveGeneration;
		setSaving(true);
		savingContent = next;
		try {
			const result = await controller.updateScratchpad(
				targetSessionId,
				expected,
				next,
			);
			if (generation !== saveGeneration || sessionId() !== targetSessionId) {
				return;
			}
			setPersistedContent(result.content);
			setError("");
			setConflicted(false);
			if (draft() === next) {
				setDirty(false);
				pendingDrafts.delete(targetSessionId);
			} else {
				rememberDraft();
				scheduleAutosave();
			}
		} catch (cause) {
			if (generation !== saveGeneration || sessionId() !== targetSessionId) {
				return;
			}
			const message = cause instanceof Error ? cause.message : String(cause);
			const conflict = message.includes("Scratchpad changed elsewhere");
			setConflicted(conflict);
			setError(conflict ? CONFLICT_ERROR : message);
			rememberDraft();
		} finally {
			if (generation === saveGeneration) {
				savingContent = null;
				setSaving(false);
				if (saveQueued) {
					saveQueued = false;
					if (!error() && !conflicted()) scheduleAutosave();
				}
			}
		}
	}

	function restorePendingDraft(targetSessionId: string): boolean {
		const pending = pendingDrafts.get(targetSessionId);
		if (!pending) return false;
		setSessionId(targetSessionId);
		setPersistedContent(pending.persistedContent);
		setDraftSignal(pending.draft);
		setDirty(true);
		setConflicted(pending.conflicted);
		setError(pending.error);
		reconciliationPending = true;
		return true;
	}

	function openScratchpad(): boolean {
		if (disabled()) return false;
		const targetSessionId = activeSessionId();
		if (!targetSessionId) return false;
		if (
			open() &&
			(sessionId() === targetSessionId ||
				loadTargetSessionId === targetSessionId)
		) {
			return true;
		}
		setOpen(true);
		restorePendingDraft(targetSessionId);
		void load(false);
		return true;
	}

	function toggle(): void {
		if (open()) {
			close();
			return;
		}
		if (document.querySelector<HTMLDialogElement>("dialog:modal")) return;
		openScratchpad();
	}

	function close(): void {
		clearAutosave();
		rememberDraft();
		void save();
		setOpen(false);
	}

	function setDraft(content: string): void {
		setDraftSignal(content);
		setDirty(content !== persistedContent());
		if (!conflicted()) setError("");
		rememberDraft();
		if (content !== persistedContent() && !conflicted()) scheduleAutosave();
		else clearAutosave();
	}

	createEffect(() => {
		const state = protocol();
		const nextSessionId = activeSessionId();
		if (nextSessionId !== observedActiveSessionId) {
			const loadedSessionId = sessionId();
			const sessionTargetChanged =
				(loadedSessionId !== null && nextSessionId !== loadedSessionId) ||
				(loadTargetSessionId !== null && nextSessionId !== loadTargetSessionId);
			if (sessionTargetChanged) {
				loadGeneration += 1;
				loadTargetSessionId = null;
				clearAutosave();
				rememberDraft();
				saveGeneration += 1;
				saveQueued = false;
				savingContent = null;
				setSaving(false);
				setOpen(false);
				setSessionId(null);
				setPersistedContent("");
				setDraftSignal("");
				setDirty(false);
				setLoading(false);
				reconciliationPending = false;
				setError("");
				setConflicted(false);
			}
			observedActiveSessionId = nextSessionId;
			if (
				nextSessionId &&
				sessionId() !== nextSessionId &&
				restorePendingDraft(nextSessionId) &&
				state.phase === "live"
			) {
				void load(false);
			}
		}

		if (state.phase === "live" && observedPhase !== "live") {
			if (
				sessionId() === null &&
				nextSessionId &&
				restorePendingDraft(nextSessionId)
			) {
				void load(false);
			} else if (reconciliationPending) {
				void load(false);
			} else if (dirty() && !conflicted()) {
				setError("");
				scheduleAutosave();
			}
		}
		observedPhase = state.phase;

		const remote = state.serverState.scratchpad;
		if (
			!isRecord(remote) ||
			typeof remote.sessionId !== "string" ||
			typeof remote.content !== "string" ||
			remote.sessionId !== sessionId() ||
			remote.content === persistedContent()
		) {
			return;
		}
		if (remote.content === savingContent) {
			setPersistedContent(remote.content);
			if (draft() === remote.content) {
				setDirty(false);
				pendingDrafts.delete(remote.sessionId);
			} else {
				rememberDraft();
			}
			return;
		}
		if (dirty()) {
			clearAutosave();
			setConflicted(true);
			setError(CONFLICT_ERROR);
			rememberDraft();
			return;
		}
		setPersistedContent(remote.content);
		setDraftSignal(remote.content);
		pendingDrafts.delete(remote.sessionId);
	});

	const confirmUnsavedScratchpad = (event: BeforeUnloadEvent) => {
		if (!dirty() && !saving() && pendingDrafts.size === 0) return;
		event.preventDefault();
		event.returnValue = "";
	};
	onMount(() => {
		window.addEventListener("beforeunload", confirmUnsavedScratchpad);
	});
	onCleanup(() => {
		loadGeneration += 1;
		loadTargetSessionId = null;
		clearAutosave();
		rememberDraft();
		window.removeEventListener("beforeunload", confirmUnsavedScratchpad);
	});

	return (
		<ScratchpadContext.Provider
			value={{
				open,
				draft,
				dirty,
				loading,
				saving,
				error,
				disabled,
				openScratchpad,
				toggle,
				close,
				setDraft,
				reload: () => {
					clearAutosave();
					void load(true);
				},
			}}
		>
			{props.children}
		</ScratchpadContext.Provider>
	);
}
