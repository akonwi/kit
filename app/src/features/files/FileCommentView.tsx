import { createHash } from "node:crypto";
import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	onCleanup,
	Show,
	untrack,
} from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { AttachmentsController } from "../../shell/attachments-controller";
import { estimateWrappedRows } from "../../shell/diff/ReviewDiffBlock";
import { ReviewNoteBox } from "../../shell/diff/ReviewNoteBox";
import { MessageComposer } from "../../shell/MessageComposer";
import { scrollbarStyle, syntaxStyle, theme } from "../../shell/theme";
import type { ToastInput } from "../../state/toasts";
import { CodeReviewAttachment } from "../review/attachment";
import {
	buildRangeNoteKey,
	buildReviewSubmission,
	countDraftNotes,
	parseRangeNoteKey,
	type ReviewDraftState,
	type ReviewRangeDraft,
} from "../review/draft";
import type {
	FileCommentDraft,
	FileCommentDraftController,
} from "./comment-draft-controller";

export type FileCommentViewProps = {
	repoRoot: string;
	path: string;
	lines: string[];
	filetype: string | undefined;
	contentColumns: number;
	lineNumberWidth: number;
	active: boolean;
	attachments: AttachmentsController;
	commentDrafts: FileCommentDraftController;
	toast: (toast: ToastInput) => void;
	onSubmitMessage: () => void | Promise<void>;
	getCurrentRevision: () => string | null;
	onFocusRequest?: () => void;
	onEditingChange?: (editing: boolean) => void;
	onNoteCountChange?: (count: number) => void;
	onStaleChange?: (stale: boolean) => void;
};

type ScrollRef = {
	scrollChildIntoView?: (id: string) => void;
};

type LineScrollRef = { scrollX: number; scrollY: number };

type EditorState =
	| { kind: "file"; value: string }
	| { kind: "range"; range: ReviewRangeDraft; value: string }
	| null;

function setMapValue(
	current: Map<string, string>,
	key: string,
	value: string,
): Map<string, string> {
	const next = new Map(current);
	const normalized = value.trim();
	if (normalized) next.set(key, normalized);
	else next.delete(key);
	return next;
}

export function fileContentRevision(lines: string[]): string {
	return createHash("sha256").update(lines.join("\n")).digest("hex");
}

export function fileReviewAttachmentId(
	repoRoot: string,
	filePath: string,
): string {
	return `file-review:${repoRoot}:${filePath}`;
}

export function draftForFile(
	filePath: string,
	state: ReviewDraftState,
): ReviewDraftState {
	const fileNotes = new Map<string, string>();
	const fileNote = state.fileNotes.get(`unchanged:${filePath}`);
	if (fileNote) fileNotes.set(`unchanged:${filePath}`, fileNote);
	const rangeNotes = new Map<string, string>();
	for (const [key, value] of state.rangeNotes) {
		if (parseRangeNoteKey(key)?.path === filePath) rangeNotes.set(key, value);
	}
	return { fileNotes, rangeNotes };
}

export function FileCommentView(props: FileCommentViewProps) {
	const draftToken = props.commentDrafts.currentToken();
	const initialDraft = props.commentDrafts.getDraft(
		draftToken,
		props.repoRoot,
		props.path,
	);
	const [fileNotes, setFileNotes] = createSignal(initialDraft.fileNotes);
	const [rangeNotes, setRangeNotes] = createSignal(initialDraft.rangeNotes);
	const [draftRevision, setDraftRevision] = createSignal(initialDraft.revision);
	const [selectedLine, setSelectedLine] = createSignal(1);
	const [rangeAnchor, setRangeAnchor] = createSignal<number | null>(null);
	const [editor, setEditor] = createSignal<EditorState>(null);
	const [submitting, setSubmitting] = createSignal(false);
	let scrollRef: ScrollRef | undefined;
	let composerRef: { plainText: string } | undefined;

	const draftState = createMemo<ReviewDraftState>(() => ({
		fileNotes: fileNotes(),
		rangeNotes: rangeNotes(),
	}));
	const currentFileDraft = createMemo<FileCommentDraft>(() => ({
		...draftForFile(props.path, draftState()),
		revision: draftRevision(),
	}));
	const currentRevision = createMemo(() => fileContentRevision(props.lines));
	const fileNoteKey = () => `unchanged:${props.path}`;
	const fileNote = createMemo(() => fileNotes().get(fileNoteKey()) ?? "");
	const noteCount = createMemo(() => countDraftNotes(currentFileDraft()));
	const stale = createMemo(
		() =>
			noteCount() > 0 &&
			!!draftRevision() &&
			draftRevision() !== currentRevision(),
	);

	function saveDraft(state: FileCommentDraft): void {
		props.commentDrafts.saveDraft(
			draftToken,
			props.repoRoot,
			props.path,
			state,
		);
	}

	function revisionFor(state: ReviewDraftState, previous: string): string {
		if (countDraftNotes(state) === 0) return "";
		return previous || currentRevision();
	}

	function applyDraft(next: FileCommentDraft): void {
		setFileNotes(next.fileNotes);
		setRangeNotes(next.rangeNotes);
		setDraftRevision(next.revision);
		saveDraft(next);
	}

	function updateFileNote(value: string): void {
		const latest = props.commentDrafts.getDraft(
			draftToken,
			props.repoRoot,
			props.path,
		);
		const state = {
			fileNotes: setMapValue(latest.fileNotes, fileNoteKey(), value),
			rangeNotes: latest.rangeNotes,
		};
		applyDraft({ ...state, revision: revisionFor(state, latest.revision) });
	}

	function updateRangeNote(range: ReviewRangeDraft, value: string): void {
		const latest = props.commentDrafts.getDraft(
			draftToken,
			props.repoRoot,
			props.path,
		);
		const state = {
			fileNotes: latest.fileNotes,
			rangeNotes: setMapValue(
				latest.rangeNotes,
				buildRangeNoteKey(range),
				value,
			),
		};
		applyDraft({ ...state, revision: revisionFor(state, latest.revision) });
	}

	onCleanup(
		props.commentDrafts.subscribe((event) => {
			if (event.type === "reset") {
				setFileNotes(new Map());
				setRangeNotes(new Map());
				setDraftRevision("");
				setEditor(null);
				return;
			}
			if (
				event.token.sessionId !== draftToken.sessionId ||
				event.token.generation !== draftToken.generation ||
				event.repoRoot !== props.repoRoot ||
				event.path !== props.path
			) {
				return;
			}
			setFileNotes(new Map(event.state.fileNotes));
			setRangeNotes(new Map(event.state.rangeNotes));
			setDraftRevision(event.state.revision);
			if (event.type === "cleared") setEditor(null);
		}),
	);

	function currentAttachment(): CodeReviewAttachment | null {
		const id = fileReviewAttachmentId(props.repoRoot, props.path);
		const attachment = props.attachments
			.attachments()
			.find((candidate) => candidate.id === id);
		return attachment instanceof CodeReviewAttachment ? attachment : null;
	}

	function queueDraft(showEmptyWarning = false): boolean {
		if (submitting()) return false;
		const submittedDraft = currentFileDraft();
		const submission = buildReviewSubmission([], submittedDraft);
		if (!submission) {
			const attached = untrack(currentAttachment);
			if (attached) props.attachments.detach(attached.id, "pending");
			if (showEmptyWarning) {
				props.toast({
					title: "No file notes",
					subtitle: "Add a file or line note before submitting.",
					variant: "warning",
				});
			}
			return false;
		}
		const freshRevision = props.getCurrentRevision();
		if (!freshRevision || submittedDraft.revision !== freshRevision) {
			const attached = untrack(currentAttachment);
			if (attached) props.attachments.detach(attached.id, "pending");
			if (showEmptyWarning) {
				props.toast({
					title: "File changed since comments were added",
					subtitle: "Clear and recreate the affected notes before submitting.",
					variant: "warning",
				});
			}
			return false;
		}
		submission.source = "file";
		props.attachments.attach(
			new CodeReviewAttachment(
				fileReviewAttachmentId(props.repoRoot, props.path),
				submission,
				{
					repoRoot: props.repoRoot,
					targetKey: `file:${props.path}`,
					validate: () =>
						props.getCurrentRevision() === submittedDraft.revision
							? null
							: "The file changed after these comments were added. Clear and recreate the stale notes.",
					onDetach: (reason) => {
						if (reason === "pending") return;
						props.commentDrafts.consumeDraft(
							draftToken,
							props.repoRoot,
							props.path,
							submittedDraft,
						);
					},
				},
			),
		);
		return true;
	}

	createEffect(() => {
		void fileNotes();
		void rangeNotes();
		void currentRevision();
		queueDraft();
	});

	createEffect(() =>
		props.onEditingChange?.(editor() !== null || rangeAnchor() !== null),
	);
	createEffect(() => props.onNoteCountChange?.(noteCount()));
	createEffect(() => props.onStaleChange?.(stale()));
	onCleanup(() => {
		props.onEditingChange?.(false);
		props.onNoteCountChange?.(0);
		props.onStaleChange?.(false);
	});

	createEffect(() => {
		const count = props.lines.length;
		setSelectedLine((line) => Math.max(1, Math.min(Math.max(1, count), line)));
	});

	createEffect(() => {
		if (!props.active) return;
		const line = selectedLine();
		queueMicrotask(() => scrollRef?.scrollChildIntoView?.(`file-line-${line}`));
	});
	createEffect(() => {
		const current = editor();
		if (!props.active || current?.kind !== "range") return;
		queueMicrotask(() =>
			scrollRef?.scrollChildIntoView?.(
				`file-comment-editor-${current.range.endLine}`,
			),
		);
	});

	const ranges = createMemo(() => {
		const values: Array<{
			key: string;
			range: ReviewRangeDraft;
			comment: string;
		}> = [];
		for (const [key, value] of rangeNotes()) {
			if (!value.trim()) continue;
			const range = parseRangeNoteKey(key);
			if (range?.path === props.path)
				values.push({ key, range, comment: value });
		}
		return values;
	});

	function rangeAtLine(line: number) {
		return ranges().find(
			({ range }) => line >= range.startLine && line <= range.endLine,
		);
	}

	function openRangeEditor(range?: ReviewRangeDraft): void {
		const existing = rangeAtLine(selectedLine());
		const nextRange = range ??
			existing?.range ?? {
				path: props.path,
				side: "additions" as const,
				startLine: selectedLine(),
				endLine: selectedLine(),
			};
		setEditor({
			kind: "range",
			range: nextRange,
			value: rangeNotes().get(buildRangeNoteKey(nextRange)) ?? "",
		});
		setRangeAnchor(null);
	}

	function confirmRange(): void {
		const anchor = rangeAnchor();
		if (anchor === null) {
			openRangeEditor();
			return;
		}
		openRangeEditor({
			path: props.path,
			side: "additions",
			startLine: Math.min(anchor, selectedLine()),
			endLine: Math.max(anchor, selectedLine()),
		});
	}

	function saveEditor(): void {
		const current = editor();
		if (!current) return;
		if (current.kind === "file") updateFileNote(current.value);
		else updateRangeNote(current.range, current.value);
		setEditor(null);
	}

	function clearSelectedRange(): void {
		const existing = rangeAtLine(selectedLine());
		if (existing) updateRangeNote(existing.range, "");
		else setRangeAnchor(null);
	}

	async function submit(): Promise<void> {
		if (submitting() || !queueDraft(true)) return;
		setSubmitting(true);
		try {
			await props.onSubmitMessage();
		} finally {
			setSubmitting(false);
		}
	}

	function selectLine(line: number, event: TuiMouseEvent): void {
		if (!props.active || event.button !== 0 || editor()) return;
		event.preventDefault();
		event.stopPropagation();
		props.onFocusRequest?.();
		setSelectedLine(line);
		confirmRange();
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active && !editor(),
		diagnosticsWhen: () => props.active && !editor(),
		commands: {
			"file-viewer.scroll-up": () => {
				setSelectedLine((line) => Math.max(1, line - 1));
			},
			"file-viewer.scroll-down": () => {
				setSelectedLine((line) =>
					Math.min(Math.max(1, props.lines.length), line + 1),
				);
			},
			"file-viewer.comment-line": confirmRange,
			"file-viewer.start-range": () => {
				setRangeAnchor(selectedLine());
			},
			"file-viewer.file-note": () => {
				setRangeAnchor(null);
				setEditor({ kind: "file", value: fileNote() });
			},
			"file-viewer.clear-line-note": clearSelectedRange,
			"file-viewer.cancel-range":
				rangeAnchor() !== null
					? () => {
							setRangeAnchor(null);
						}
					: undefined,
			"file-viewer.submit": () => void submit(),
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active && !!editor(),
		diagnosticsWhen: () => props.active && !!editor(),
		commands: {
			"file-viewer.close-editor": () => {
				setEditor(null);
			},
		},
	}));

	function updateEditorValue(value: string): void {
		setEditor((current) => (current ? { ...current, value } : null));
	}

	function resetLineScroll(ref: LineScrollRef | undefined): void {
		queueMicrotask(() => {
			if (!ref) return;
			ref.scrollX = 0;
			ref.scrollY = 0;
		});
	}

	return (
		<box flexGrow={1} flexDirection="column" overflow="hidden">
			<Show when={stale()}>
				<box flexShrink={0} paddingX={1} backgroundColor={theme.bgSurface}>
					<text fg={theme.warningText} bg={theme.bgSurface}>
						File changed · comments are stale
					</text>
				</box>
			</Show>
			<Show when={editor()?.kind === "file" || fileNote()}>
				<Show
					when={editor()?.kind === "file"}
					fallback={
						<ReviewNoteBox marginX={1}>
							<text fg={theme.textPrimary} bg={theme.bgSurface}>
								{fileNote()}
							</text>
						</ReviewNoteBox>
					}
				>
					<box flexShrink={0} marginX={1}>
						<MessageComposer
							initialValue={editor()?.value ?? ""}
							placeholder="Comment on the whole file..."
							backgroundColor={theme.bgTransparent}
							focusedBackgroundColor={theme.bgTransparent}
							focused={props.active}
							showCursor={props.active}
							keyBindings={[
								{ name: "return", action: "submit" },
								{ name: "return", shift: true, action: "newline" },
							]}
							onContentChange={() =>
								updateEditorValue(composerRef?.plainText ?? "")
							}
							onSubmit={saveEditor}
							ref={(value) => {
								composerRef = value as typeof composerRef;
							}}
						/>
					</box>
				</Show>
			</Show>

			<scrollbox
				ref={(value) => {
					scrollRef = value as ScrollRef;
				}}
				flexGrow={1}
				scrollY
				style={scrollbarStyle()}
			>
				{/* Keep rows direct: OpenTUI culls only direct scrollbox children. */}
				<For each={props.lines}>
					{(line, index) => {
						const lineNumber = () => index() + 1;
						const selected = () =>
							props.active && lineNumber() === selectedLine();
						const inRange = () => {
							const anchor = rangeAnchor();
							if (anchor !== null) {
								return (
									lineNumber() >= Math.min(anchor, selectedLine()) &&
									lineNumber() <= Math.max(anchor, selectedLine())
								);
							}
							return !!rangeAtLine(lineNumber());
						};
						const rowBackground = () =>
							selected() || inRange() ? theme.diffCursorBg : theme.bg;
						const gutterBackground = () =>
							selected() ? theme.diffCursorGutterBg : rowBackground();
						const rowHeight = () =>
							Math.max(1, estimateWrappedRows(line, props.contentColumns));
						const annotation = () =>
							ranges().find(({ range }) => range.endLine === lineNumber()) ??
							null;
						const editingRange = () => {
							const current = editor();
							return current?.kind === "range" &&
								current.range.endLine === lineNumber()
								? current
								: null;
						};
						let lineRef: LineScrollRef | undefined;
						return (
							<>
								<box
									id={`file-line-${lineNumber()}`}
									flexDirection="row"
									alignItems="flex-start"
									height={rowHeight()}
									flexShrink={0}
									backgroundColor={rowBackground()}
									onMouseDown={(event) => selectLine(lineNumber(), event)}
								>
									<text
										fg={theme.textMuted}
										bg={gutterBackground()}
										flexShrink={0}
										height={rowHeight()}
									>
										{String(lineNumber()).padStart(props.lineNumberWidth)}
										{"  "}
									</text>
									<Show
										when={props.filetype}
										fallback={
											<text
												ref={(value) => {
													lineRef = value as LineScrollRef;
												}}
												fg={theme.textPrimary}
												bg={rowBackground()}
												wrapMode="word"
												flexGrow={1}
												height={rowHeight()}
												onMouseScroll={() => resetLineScroll(lineRef)}
											>
												{line}
											</text>
										}
									>
										{(type) => (
											<code
												ref={(value) => {
													lineRef = value as LineScrollRef;
												}}
												content={line}
												filetype={type()}
												syntaxStyle={syntaxStyle()}
												bg={rowBackground()}
												conceal={false}
												wrapMode="word"
												flexGrow={1}
												height={rowHeight()}
												onMouseScroll={() => resetLineScroll(lineRef)}
											/>
										)}
									</Show>
								</box>
								<Show when={editingRange() ?? annotation()}>
									{(note) => (
										<Show
											when={"value" in note()}
											fallback={
												<ReviewNoteBox>
													<text fg={theme.textPrimary} bg={theme.bgSurface}>
														{annotation()?.comment}
													</text>
												</ReviewNoteBox>
											}
										>
											<box
												id={`file-comment-editor-${lineNumber()}`}
												flexShrink={0}
											>
												<MessageComposer
													initialValue={editor()?.value ?? ""}
													placeholder="Type your review note..."
													backgroundColor={theme.bgTransparent}
													focusedBackgroundColor={theme.bgTransparent}
													focused={props.active}
													showCursor={props.active}
													keyBindings={[
														{ name: "return", action: "submit" },
														{
															name: "return",
															shift: true,
															action: "newline",
														},
													]}
													onContentChange={() =>
														updateEditorValue(composerRef?.plainText ?? "")
													}
													onSubmit={saveEditor}
													ref={(value) => {
														composerRef = value as typeof composerRef;
													}}
												/>
											</box>
										</Show>
									)}
								</Show>
							</>
						);
					}}
				</For>
			</scrollbox>
		</box>
	);
}
