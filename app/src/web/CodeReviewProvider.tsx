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
import type {
	RemoteReviewFile,
	RemoteReviewNote,
	RemoteReviewState,
} from "./remote-services";
import { RpcResponseLostError } from "./rpc-transport";
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
	submitting: Accessor<boolean>;
	staleTarget: Accessor<boolean>;
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
	submitReview(): Promise<void>;
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
	const [submitting, setSubmitting] = createSignal(false);
	const [staleTarget, setStaleTarget] = createSignal(false);
	const [error, setError] = createSignal("");
	const [state, setState] = createSignal<RemoteReviewState | null>(null);
	const [selectedFile, setSelectedFile] = createSignal<RemoteReviewFile | null>(
		null,
	);
	const [notes, setNotes] = createSignal<LocalReviewNote[]>([]);
	const [draft, setDraft] = createSignal<LocalReviewNoteDraft | null>(null);
	let generation = 0;
	let observedSessionId: unknown;
	let pendingSubmission: {
		id: string;
		notes: LocalReviewNote[];
		remoteNotes: RemoteReviewNote[];
		sessionId: string;
		repoRoot: string;
		generation: number;
	} | null = null;
	let refreshAfterSubmission = false;

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
			const previous = state();
			const targetChanged =
				previous !== null &&
				(previous.sessionId !== next.sessionId ||
					previous.repoRoot !== next.repoRoot);
			if (targetChanged && submitting()) return;
			if (targetChanged) {
				const hadLocalDraft = notes().length > 0 || draft() !== null;
				setNotes([]);
				setDraft(null);
				pendingSubmission = null;
				if (hadLocalDraft) {
					setError("Review target changed; local notes were cleared");
				}
			}
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
		if (!staleTarget()) void refresh();
	}

	function close(): void {
		generation += 1;
		setOpen(false);
		setError("");
		setDraft(null);
	}

	function selectRange(path: string, range: SelectedLineRange | null): void {
		if (submitting()) return;
		const current = draft();
		if (current?.initialComment.trim()) {
			// Keep an in-progress comment and ask Pierre to restore its selection.
			setDraft({ ...current });
			return;
		}
		setDraft(range ? { path, range, initialComment: "" } : null);
	}

	function saveNote(value: LocalReviewNoteDraft, comment: string): void {
		if (submitting()) return;
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
		if (submitting()) return;
		setDraft({
			path: note.path,
			range: note.range,
			noteId: note.id,
			initialComment: note.comment,
		});
	}

	function updateDraftComment(comment: string): void {
		if (submitting()) return;
		const current = draft();
		if (current) current.initialComment = comment;
	}

	function deleteNote(noteId: string): void {
		if (submitting()) return;
		setNotes((current) => current.filter((note) => note.id !== noteId));
		if (draft()?.noteId === noteId) setDraft(null);
	}

	function cancelNote(): void {
		if (submitting()) return;
		setDraft(null);
	}

	async function submitReview(): Promise<void> {
		const currentState = state();
		const submittedNotes = notes();
		if (
			!currentState ||
			submittedNotes.length === 0 ||
			submitting() ||
			staleTarget()
		) {
			return;
		}
		if (draft()) {
			setError("Add or cancel the current note before submitting");
			return;
		}

		const remoteNotes: RemoteReviewNote[] = [];
		for (const note of submittedNotes) {
			const side = note.range.side;
			const endSide = note.range.endSide ?? side;
			if ((side !== "additions" && side !== "deletions") || endSide !== side) {
				setError("Review notes must stay on one side of the diff");
				return;
			}
			remoteNotes.push({
				path: note.path,
				side,
				startLine: Math.min(note.range.start, note.range.end),
				endLine: Math.max(note.range.start, note.range.end),
				comment: note.comment,
			});
		}

		const reusableSubmission =
			pendingSubmission?.notes.length === submittedNotes.length &&
			pendingSubmission.notes.every(
				(note, index) => note === submittedNotes[index],
			);
		const submission =
			reusableSubmission && pendingSubmission
				? pendingSubmission
				: {
						id: crypto.randomUUID(),
						notes: submittedNotes,
						remoteNotes,
						sessionId: currentState.sessionId,
						repoRoot: currentState.repoRoot,
						generation: currentState.generation,
					};
		pendingSubmission = submission;
		setSubmitting(true);
		setError("");
		try {
			await controller.submitReview(
				submission.id,
				submission.sessionId,
				submission.generation,
				submission.remoteNotes,
			);
			setNotes((current) =>
				current.filter((note) => !submittedNotes.includes(note)),
			);
			pendingSubmission = null;
		} catch (cause) {
			if (!(cause instanceof RpcResponseLostError)) pendingSubmission = null;
			const message = cause instanceof Error ? cause.message : String(cause);
			let targetChanged =
				snapshot().protocol.serverState.sessionId !== submission.sessionId;
			if (!targetChanged && refreshAfterSubmission) {
				try {
					const latest = await controller.getReviewState();
					targetChanged =
						latest.sessionId !== submission.sessionId ||
						latest.repoRoot !== submission.repoRoot;
				} catch {
					targetChanged = true;
				}
			}
			if (targetChanged) {
				setStaleTarget(true);
				refreshAfterSubmission = false;
				setError(`${message}. Local notes were kept for recovery.`);
			} else {
				setError(message);
			}
		} finally {
			setSubmitting(false);
			if (refreshAfterSubmission && open()) {
				refreshAfterSubmission = false;
				void refresh();
			}
		}
	}

	async function selectFile(path: string): Promise<void> {
		if (!open() || submitting()) return;
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
		const localNoteCount = notes().length;
		if (observedSessionId !== undefined && observedSessionId !== sessionId) {
			if (submitting() || (staleTarget() && localNoteCount > 0)) return;
			close();
			setState(null);
			setSelectedFile(null);
			setNotes([]);
			setDraft(null);
			setStaleTarget(false);
			pendingSubmission = null;
		}
		observedSessionId = sessionId;
	});

	createEffect(() => {
		if (!staleTarget() || notes().length > 0 || submitting()) return;
		setStaleTarget(false);
		if (open() && snapshot().protocol.phase === "live") void refresh();
	});

	const unsubscribeReview = controller.subscribeReview(() => {
		if (!open() || staleTarget() || snapshot().protocol.phase !== "live") {
			return;
		}
		if (submitting()) {
			refreshAfterSubmission = true;
			return;
		}
		void refresh();
	});
	onCleanup(unsubscribeReview);

	return (
		<CodeReviewContext.Provider
			value={{
				open,
				loading,
				submitting,
				staleTarget,
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
				submitReview,
			}}
		>
			{props.children}
		</CodeReviewContext.Provider>
	);
}
