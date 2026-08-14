/** @jsxImportSource solid-js */
import type { SelectedLineRange } from "@pierre/diffs";
import {
	type Accessor,
	createContext,
	createEffect,
	createSignal,
	type JSX,
	onCleanup,
	useContext,
} from "solid-js";
import type { RemoteReviewFile, RemoteReviewState } from "./remote-services";
import { useWebClient } from "./WebClientContext";

export type LocalReviewNote = {
	id: string;
	path: string;
	range: SelectedLineRange;
	comment: string;
};

export type LocalReviewNoteDraft = {
	path: string;
	range: SelectedLineRange;
	noteId?: string;
	initialComment: string;
};

type CodeReviewContextValue = {
	open: Accessor<boolean>;
	loading: Accessor<boolean>;
	error: Accessor<string>;
	state: Accessor<RemoteReviewState | null>;
	selectedFile: Accessor<RemoteReviewFile | null>;
	notes: Accessor<LocalReviewNote[]>;
	draft: Accessor<LocalReviewNoteDraft | null>;
	openReview(): void;
	close(): void;
	selectFile(path: string): Promise<void>;
	refresh(): Promise<void>;
	selectRange(path: string, range: SelectedLineRange | null): void;
	saveNote(draft: LocalReviewNoteDraft, comment: string): void;
	editNote(note: LocalReviewNote): void;
	updateDraftComment(comment: string): void;
	deleteNote(noteId: string): void;
	cancelNote(): void;
};

const CodeReviewContext = createContext<CodeReviewContextValue>();

export function useCodeReview(): CodeReviewContextValue {
	const value = useContext(CodeReviewContext);
	if (!value) throw new Error("CodeReviewProvider is missing");
	return value;
}

export function CodeReviewProvider(props: {
	children: JSX.Element;
}): JSX.Element {
	const { controller, snapshot } = useWebClient();
	const [open, setOpen] = createSignal(false);
	const [loading, setLoading] = createSignal(false);
	const [error, setError] = createSignal("");
	const [state, setState] = createSignal<RemoteReviewState | null>(null);
	const [selectedFile, setSelectedFile] = createSignal<RemoteReviewFile | null>(
		null,
	);
	const [notes, setNotes] = createSignal<LocalReviewNote[]>([]);
	const [draft, setDraft] = createSignal<LocalReviewNoteDraft | null>(null);
	let generation = 0;
	let observedSessionId: unknown;

	async function loadFile(path: string, loadGeneration: number): Promise<void> {
		const file = await controller.getReviewFile(path);
		if (loadGeneration !== generation || !open()) return;
		setSelectedFile(file);
	}

	async function refresh(): Promise<void> {
		if (!open()) return;
		const loadGeneration = ++generation;
		setLoading(true);
		setError("");
		try {
			const next = await controller.getReviewState();
			if (loadGeneration !== generation || !open()) return;
			setState(next);
			const currentPath = selectedFile()?.file.path;
			const path = next.files.some((file) => file.path === currentPath)
				? currentPath
				: next.files[0]?.path;
			if (path) await loadFile(path, loadGeneration);
			else setSelectedFile(null);
		} catch (cause) {
			if (loadGeneration !== generation) return;
			setError(cause instanceof Error ? cause.message : String(cause));
		} finally {
			if (loadGeneration === generation) setLoading(false);
		}
	}

	function openReview(): void {
		if (snapshot().protocol.phase !== "live") return;
		setOpen(true);
		void refresh();
	}

	function close(): void {
		generation += 1;
		setOpen(false);
		setError("");
		setDraft(null);
	}

	function selectRange(path: string, range: SelectedLineRange | null): void {
		const current = draft();
		if (current?.initialComment.trim()) {
			// Keep an in-progress comment and ask Pierre to restore its selection.
			setDraft({ ...current });
			return;
		}
		setDraft(range ? { path, range, initialComment: "" } : null);
	}

	function saveNote(value: LocalReviewNoteDraft, comment: string): void {
		const normalized = comment.trim();
		if (!normalized) return;
		if (value.noteId) {
			setNotes((current) =>
				current.map((note) =>
					note.id === value.noteId
						? { ...note, range: value.range, comment: normalized }
						: note,
				),
			);
		} else {
			setNotes((current) => [
				...current,
				{
					id: crypto.randomUUID(),
					path: value.path,
					range: value.range,
					comment: normalized,
				},
			]);
		}
		setDraft(null);
	}

	function editNote(note: LocalReviewNote): void {
		setDraft({
			path: note.path,
			range: note.range,
			noteId: note.id,
			initialComment: note.comment,
		});
	}

	function updateDraftComment(comment: string): void {
		const current = draft();
		if (current) current.initialComment = comment;
	}

	function deleteNote(noteId: string): void {
		setNotes((current) => current.filter((note) => note.id !== noteId));
		if (draft()?.noteId === noteId) setDraft(null);
	}

	function cancelNote(): void {
		setDraft(null);
	}

	async function selectFile(path: string): Promise<void> {
		if (!open()) return;
		const loadGeneration = ++generation;
		setLoading(true);
		setError("");
		try {
			await loadFile(path, loadGeneration);
		} catch (cause) {
			if (loadGeneration !== generation) return;
			setError(cause instanceof Error ? cause.message : String(cause));
		} finally {
			if (loadGeneration === generation) setLoading(false);
		}
	}

	createEffect(() => {
		const sessionId = snapshot().protocol.serverState.sessionId;
		if (observedSessionId !== undefined && observedSessionId !== sessionId) {
			close();
			setState(null);
			setSelectedFile(null);
			setNotes([]);
			setDraft(null);
		}
		observedSessionId = sessionId;
	});

	const unsubscribeReview = controller.subscribeReview(() => {
		if (open() && snapshot().protocol.phase === "live") void refresh();
	});
	onCleanup(unsubscribeReview);

	return (
		<CodeReviewContext.Provider
			value={{
				open,
				loading,
				error,
				state,
				selectedFile,
				notes,
				draft,
				openReview,
				close,
				selectFile,
				refresh,
				selectRange,
				saveNote,
				editNote,
				updateDraftComment,
				deleteNote,
				cancelNote,
			}}
		>
			{props.children}
		</CodeReviewContext.Provider>
	);
}
