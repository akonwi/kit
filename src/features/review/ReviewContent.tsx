import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	For,
	Show,
} from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { AttachmentsController } from "../../shell/attachments-controller";
import { type Binding, HintBar } from "../../shell/HintBar";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { syntaxStyle, theme } from "../../shell/theme";
import type { ToastInput } from "../../state/toasts";
import { CodeReviewAttachment } from "../code-review/attachment";
import {
	buildRangeNoteKey,
	buildReviewSubmission,
	countDraftNotes,
	countFileDraftNotes,
	parseRangeNoteKey,
	type ReviewDraftState,
	type ReviewRangeDraft,
} from "./draft";
import {
	loadReviewFiles,
	type ReviewFile,
	type ReviewHunk,
	type ReviewLine,
} from "./model";
import { ReviewNoteModal } from "./ReviewNoteModal";

export type ReviewContentProps = {
	onClose: () => void;
	attachments: AttachmentsController;
	toast: (toast: ToastInput) => void;
	openCustomOverlay: <T>(
		component: (
			props: OverlayComponentProps<T>,
		) => import("solid-js").JSX.Element,
	) => Promise<T>;
	surfaceProps?: OverlayComponentProps<void>["surfaceProps"];
};

type ReviewMode = "list" | "patch";
type ReviewSide = "additions" | "deletions";
type CommentableLine = {
	index: number;
	side: ReviewSide;
	lineNumber: number;
	text: string;
	kind: Extract<ReviewLine["kind"], "add" | "delete">;
};
type RangeAnchor = {
	side: ReviewSide;
	lineNumber: number;
};

const FOCUS_BINDINGS: { [key in ReviewMode]: Binding[] } = {
	list: [
		{ key: "↑/↓ or j/k", action: "move" },
		{ key: "Enter", action: "focus change group" },
		{ key: "Space", action: "collapse/expand" },
		{ key: "f", action: "file note" },
		{ key: "x", action: "clear file note" },
		{ key: "s", action: "submit" },
		{ key: "Esc", action: "close" },
	],
	patch: [
		{ key: "↑/↓ or j/k", action: "move line" },
		{ key: "Tab / Shift+Tab", action: "change group" },
		{ key: "Enter", action: "comment line / confirm range" },
		{ key: "Ctrl+Enter", action: "start range" },
		{ key: "x", action: "clear line note" },
		{ key: "f", action: "file note" },
		{ key: "s", action: "submit" },
		{ key: "Esc", action: "cancel range / back" },
	],
};

function statusLabel(file: ReviewFile): string {
	switch (file.status) {
		case "new":
			return "A";
		case "deleted":
			return "D";
		case "rename-pure":
		case "rename-changed":
			return "R";
		default:
			return "M";
	}
}

type ScrollRef = {
	scrollChildIntoView: (childId: string) => void;
	scrollBy: (delta: number | { x: number; y: number }) => void;
};

type PatchScrollRef = {
	scrollBy: (delta: number | { x: number; y: number }) => void;
	scrollChildIntoView?: (childId: string) => void;
};

function formatNoteCount(count: number): string {
	return `${count} note${count === 1 ? "" : "s"}`;
}

function setMapValue(
	map: Map<string, string>,
	key: string,
	value: string,
): Map<string, string> {
	const next = new Map(map);
	if (value.trim().length === 0) next.delete(key);
	else next.set(key, value);
	return next;
}

function getCommentableLines(
	hunk: ReviewHunk,
	side?: ReviewSide,
): CommentableLine[] {
	const lines: CommentableLine[] = [];
	for (const [index, line] of hunk.lines.entries()) {
		if (line.kind === "add" && line.additionLineNumber != null) {
			if (!side || side === "additions") {
				lines.push({
					index,
					side: "additions",
					lineNumber: line.additionLineNumber,
					text: line.text,
					kind: "add",
				});
			}
			continue;
		}
		if (line.kind === "delete" && line.deletionLineNumber != null) {
			if (!side || side === "deletions") {
				lines.push({
					index,
					side: "deletions",
					lineNumber: line.deletionLineNumber,
					text: line.text,
					kind: "delete",
				});
			}
		}
	}
	return lines;
}

function lineRangeLabel(range: ReviewRangeDraft): string {
	const startLine = Math.min(range.startLine, range.endLine);
	const endLine = Math.max(range.startLine, range.endLine);
	return startLine === endLine
		? `${range.side} ${startLine}`
		: `${range.side} ${startLine}-${endLine}`;
}

function buildRangeMarker(height: number): string {
	return Array.from({ length: Math.max(1, height) }, () => "┆").join("\n");
}

function buildLineSelection(
	path: string,
	anchor: RangeAnchor,
	line: CommentableLine,
): ReviewRangeDraft | null {
	if (line.side !== anchor.side) return null;
	return {
		path,
		side: line.side,
		startLine: Math.min(anchor.lineNumber, line.lineNumber),
		endLine: Math.max(anchor.lineNumber, line.lineNumber),
	};
}

function findSavedRangeAtLine(
	path: string,
	line: CommentableLine,
	rangeNotes: Map<string, string>,
): ReviewRangeDraft | null {
	let bestMatch: ReviewRangeDraft | null = null;
	for (const [key, value] of rangeNotes) {
		if (!value.trim()) continue;
		const range = parseRangeNoteKey(key);
		if (!range) continue;
		if (range.path !== path || range.side !== line.side) continue;
		if (line.lineNumber < range.startLine || line.lineNumber > range.endLine) {
			continue;
		}
		if (!bestMatch) {
			bestMatch = range;
			continue;
		}
		const bestSpan = bestMatch.endLine - bestMatch.startLine;
		const rangeSpan = range.endLine - range.startLine;
		if (
			rangeSpan < bestSpan ||
			(rangeSpan === bestSpan && range.startLine < bestMatch.startLine)
		) {
			bestMatch = range;
		}
	}
	return bestMatch;
}

export function ReviewContent(props: ReviewContentProps) {
	const [files] = createResource(() => loadReviewFiles());
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [expandedKeys, setExpandedKeys] = createSignal<Set<string>>(new Set());
	const [mode, setMode] = createSignal<ReviewMode>("list");
	const [fileNotes, setFileNotes] = createSignal<Map<string, string>>(
		new Map(),
	);
	const [rangeNotes, setRangeNotes] = createSignal<Map<string, string>>(
		new Map(),
	);
	const [selectedHunkIndices, setSelectedHunkIndices] = createSignal<
		Map<string, number>
	>(new Map());
	const [selectedLineIndices, setSelectedLineIndices] = createSignal<
		Map<string, number>
	>(new Map());
	const [rangeAnchor, setRangeAnchor] = createSignal<RangeAnchor | null>(null);
	const [editorOpen, setEditorOpen] = createSignal(false);
	const patchScrollRefs = new Map<string, PatchScrollRef>();
	let listScrollRef: ScrollRef | undefined;
	let patchCursorScrollTimeout: ReturnType<typeof setTimeout> | undefined;

	const reviewFiles = createMemo(() => files() ?? []);
	const draftState = createMemo<ReviewDraftState>(() => ({
		fileNotes: fileNotes(),
		rangeNotes: rangeNotes(),
	}));
	const totalDraftNotes = createMemo(() => countDraftNotes(draftState()));
	const selectedFile = createMemo(() => reviewFiles()[selectedIndex()] ?? null);
	const selectedHunk = createMemo(() => {
		const file = selectedFile();
		if (!file || file.hunks.length === 0) return null;
		const index = Math.max(
			0,
			Math.min(file.hunks.length - 1, selectedHunkIndices().get(file.id) ?? 0),
		);
		return file.hunks[index] ?? null;
	});
	const selectedHunkIndex = createMemo(() => {
		const file = selectedFile();
		const hunk = selectedHunk();
		if (!file || !hunk) return -1;
		return file.hunks.findIndex((candidate) => candidate.id === hunk.id);
	});
	const selectedCommentableLines = createMemo(() => {
		const hunk = selectedHunk();
		if (!hunk) return [];
		return getCommentableLines(hunk, rangeAnchor()?.side);
	});
	const selectedLine = createMemo(() => {
		const hunk = selectedHunk();
		if (!hunk) return null;
		const lines = selectedCommentableLines();
		if (lines.length === 0) return null;
		const index = Math.max(
			0,
			Math.min(lines.length - 1, selectedLineIndices().get(hunk.id) ?? 0),
		);
		return lines[index] ?? null;
	});
	const selectedSavedRange = createMemo(() => {
		const file = selectedFile();
		const line = selectedLine();
		if (!file || !line || rangeAnchor()) return null;
		return findSavedRangeAtLine(file.path, line, rangeNotes());
	});
	const selectedRange = createMemo(() => {
		const file = selectedFile();
		const line = selectedLine();
		if (!file || !line) return null;
		const anchor = rangeAnchor();
		if (!anchor) {
			return (
				selectedSavedRange() ??
				({
					path: file.path,
					side: line.side,
					startLine: line.lineNumber,
					endLine: line.lineNumber,
				} satisfies ReviewRangeDraft)
			);
		}
		return buildLineSelection(file.path, anchor, line);
	});
	const selectedRangeNote = createMemo(() => {
		const range = selectedRange();
		if (!range) return "";
		return rangeNotes().get(buildRangeNoteKey(range))?.trim() ?? "";
	});
	const currentLineNoteLabel = createMemo(() => {
		const range = selectedRange();
		if (!range) return "";
		return range.startLine === range.endLine ? "Line note" : "Range note";
	});
	const activeLineStatus = createMemo(() => {
		const range = selectedRange();
		if (!range || mode() !== "patch") return "";
		if (rangeAnchor()) {
			return `Selecting ${lineRangeLabel(range)} · press Enter to comment`;
		}
		return `Selected ${lineRangeLabel(range)} · press Ctrl+Enter to start a range`;
	});
	const lineCursorState = createMemo(() => {
		const hunk = selectedHunk();
		const line = selectedLine();
		if (mode() !== "patch" || !hunk || !line) return null;
		return { hunk, line };
	});
	const anchorLineIndex = createMemo(() => {
		const anchor = rangeAnchor();
		const hunk = selectedHunk();
		if (!anchor || !hunk) return null;
		for (const [index, line] of hunk.lines.entries()) {
			const side =
				line.kind === "add"
					? "additions"
					: line.kind === "delete"
						? "deletions"
						: undefined;
			const lineNumber =
				line.kind === "add"
					? line.additionLineNumber
					: line.kind === "delete"
						? line.deletionLineNumber
						: undefined;
			if (side === anchor.side && lineNumber === anchor.lineNumber)
				return index;
		}
		return null;
	});
	const activeRangeLineBounds = createMemo(() => {
		const range = selectedRange();
		const anchor = rangeAnchor();
		const hunk = selectedHunk();
		if (!range || !anchor || !hunk) return null;
		let startIndex: number | null = null;
		let endIndex: number | null = null;
		for (const [index, line] of hunk.lines.entries()) {
			const side =
				line.kind === "add"
					? "additions"
					: line.kind === "delete"
						? "deletions"
						: undefined;
			const lineNumber =
				line.kind === "add"
					? line.additionLineNumber
					: line.kind === "delete"
						? line.deletionLineNumber
						: undefined;
			if (!side || lineNumber == null || side !== range.side) continue;
			if (lineNumber < range.startLine || lineNumber > range.endLine) continue;
			if (startIndex == null) startIndex = index;
			endIndex = index;
		}
		if (startIndex == null || endIndex == null) return null;
		return { startIndex, endIndex };
	});

	createEffect(() => {
		const list = reviewFiles();
		if (selectedIndex() >= list.length) {
			setSelectedIndex(Math.max(0, list.length - 1));
		}
	});

	createEffect(() => {
		const list = reviewFiles();
		if (list.length === 0) {
			setExpandedKeys(new Set<string>());
			setMode("list");
			setRangeAnchor(null);
			return;
		}
		setExpandedKeys((prev) => {
			if (prev.size > 0) return prev;
			return new Set<string>(list.map((file) => file.id));
		});
	});

	createEffect(() => {
		if (mode() !== "list") return;
		const file = selectedFile();
		if (!file) return;
		listScrollRef?.scrollChildIntoView(`review-file-${file.id}`);
	});

	createEffect(() => {
		const hunk = selectedHunk();
		if (!hunk) {
			setRangeAnchor(null);
			return;
		}
		const lines = selectedCommentableLines();
		if (lines.length === 0) return;
		setSelectedLineIndices((prev) => {
			const next = new Map(prev);
			const current = next.get(hunk.id) ?? 0;
			next.set(hunk.id, Math.max(0, Math.min(lines.length - 1, current)));
			return next;
		});
	});

	createEffect(() => {
		if (mode() !== "patch") return;
		const hunk = selectedHunk();
		const line = selectedLine();
		const file = selectedFile();
		if (!hunk || !line || !file) return;
		if (patchCursorScrollTimeout) clearTimeout(patchCursorScrollTimeout);
		patchCursorScrollTimeout = setTimeout(() => {
			patchScrollRefs
				.get(file.id)
				?.scrollChildIntoView?.(`review-line-cursor-${hunk.id}-${line.index}`);
		}, 0);
	});

	function selectedFileNote(file: ReviewFile): string {
		return fileNotes().get(file.noteKey)?.trim() ?? "";
	}

	function setSelectedHunkIndex(fileId: string, index: number) {
		setSelectedHunkIndices((prev) => {
			const next = new Map(prev);
			next.set(fileId, index);
			return next;
		});
		const hunk = selectedFile()?.hunks[index];
		if (hunk) {
			setSelectedLineIndex(hunk.id, 0);
		}
		setRangeAnchor(null);
	}

	function setSelectedLineIndex(hunkId: string, index: number) {
		setSelectedLineIndices((prev) => {
			const next = new Map(prev);
			next.set(hunkId, index);
			return next;
		});
	}

	function cycleHunk(delta: number) {
		const file = selectedFile();
		if (!file || file.hunks.length === 0) return;
		const current = selectedHunkIndex();
		const nextIndex = Math.max(
			0,
			Math.min(file.hunks.length - 1, current + delta),
		);
		if (nextIndex === current) return;
		setSelectedHunkIndex(file.id, nextIndex);
	}

	function moveSelectedLine(delta: number) {
		if (delta === 0) return;
		const file = selectedFile();
		const hunk = selectedHunk();
		const lines = selectedCommentableLines();
		if (!file || !hunk || lines.length === 0) return;
		const currentLine = selectedLine();
		const current = currentLine ? lines.indexOf(currentLine) : -1;
		const nextIndex = (current >= 0 ? current : 0) + delta;
		if (nextIndex >= 0 && nextIndex < lines.length) {
			setSelectedLineIndex(hunk.id, nextIndex);
			return;
		}

		if (rangeAnchor()) return;

		const direction = delta > 0 ? 1 : -1;
		for (
			let hunkIndex = selectedHunkIndex() + direction;
			hunkIndex >= 0 && hunkIndex < file.hunks.length;
			hunkIndex += direction
		) {
			const candidate = file.hunks[hunkIndex];
			const candidateLines = getCommentableLines(candidate);
			if (candidateLines.length === 0) continue;
			setSelectedHunkIndex(file.id, hunkIndex);
			setSelectedLineIndex(
				candidate.id,
				direction > 0 ? 0 : candidateLines.length - 1,
			);
			return;
		}
	}

	function toggleExpanded(fileId: string) {
		setExpandedKeys((prev) => {
			const next = new Set(prev);
			if (next.has(fileId)) {
				next.delete(fileId);
				if (selectedFile()?.id === fileId) {
					setMode("list");
					setRangeAnchor(null);
				}
			} else {
				next.add(fileId);
			}
			return next;
		});
	}

	async function openFileNoteEditor(file: ReviewFile) {
		setEditorOpen(true);
		try {
			const nextValue = await props.openCustomOverlay<string | null>(
				(overlayProps) => (
					<ReviewNoteModal
						surfaceProps={overlayProps.surfaceProps}
						title={`File note · ${file.path}`}
						subtitle={file.prevPath ? `from ${file.prevPath}` : undefined}
						initialValue={selectedFileNote(file)}
						placeholder="Comment on the whole file..."
						onClose={overlayProps.done}
					/>
				),
			);
			if (nextValue === null) return;
			setFileNotes((prev) => setMapValue(prev, file.noteKey, nextValue));
		} finally {
			setEditorOpen(false);
		}
	}

	async function openRangeNoteEditor(
		file: ReviewFile,
		range: ReviewRangeDraft,
	) {
		setEditorOpen(true);
		try {
			const key = buildRangeNoteKey(range);
			const nextValue = await props.openCustomOverlay<string | null>(
				(overlayProps) => (
					<ReviewNoteModal
						surfaceProps={overlayProps.surfaceProps}
						title={`${range.startLine === range.endLine ? "Line" : "Range"} note · ${file.path}`}
						subtitle={lineRangeLabel(range)}
						initialValue={rangeNotes().get(key) ?? ""}
						placeholder="Comment on the selected line or range..."
						onClose={overlayProps.done}
					/>
				),
			);
			if (nextValue === null) return;
			setRangeNotes((prev) => setMapValue(prev, key, nextValue));
		} finally {
			setEditorOpen(false);
			setRangeAnchor(null);
		}
	}

	function clearSelectedFileNote() {
		const file = selectedFile();
		if (!file) return;
		setFileNotes((prev) => setMapValue(prev, file.noteKey, ""));
	}

	function clearSelectedRangeNote() {
		const range = selectedRange();
		if (!range) return;
		setRangeNotes((prev) => setMapValue(prev, buildRangeNoteKey(range), ""));
	}

	function submitReview() {
		const submission = buildReviewSubmission(reviewFiles(), draftState());
		if (!submission) {
			props.toast({
				title: "No review notes",
				lines: ["Add a file or line note before submitting review."],
				variant: "warning",
			});
			return;
		}
		props.attachments.attach(
			new CodeReviewAttachment("code-review", submission),
		);
		props.toast({
			title: "Code review attached",
			lines: [
				`Attached ${formatNoteCount(totalDraftNotes())} across ${submission.files.length} file${submission.files.length === 1 ? "" : "s"}.`,
			],
			variant: "info",
		});
		props.onClose();
	}

	function beginRangeSelection() {
		const line = selectedLine();
		if (!line) return;
		setRangeAnchor({ side: line.side, lineNumber: line.lineNumber });
	}

	function clearOrCancelLineSelection() {
		if (rangeAnchor()) {
			setRangeAnchor(null);
			return;
		}
		clearSelectedRangeNote();
	}

	function confirmSelectedLineComment() {
		const file = selectedFile();
		const range = selectedRange();
		if (!file || !range) return;
		void openRangeNoteEditor(file, range);
	}

	useKeyboard((e: KeyEvent) => {
		if (editorOpen()) return;
		if (mode() === "patch") {
			if (e.name === "escape") {
				e.preventDefault();
				if (rangeAnchor()) setRangeAnchor(null);
				else setMode("list");
				return;
			}
			if (e.shift && e.name === "tab") {
				e.preventDefault();
				cycleHunk(-1);
				return;
			}
			if (e.name === "tab") {
				e.preventDefault();
				cycleHunk(1);
				return;
			}
			if (e.name === "k" || e.name === "up") {
				e.preventDefault();
				moveSelectedLine(-1);
				return;
			}
			if (e.name === "j" || e.name === "down") {
				e.preventDefault();
				moveSelectedLine(1);
				return;
			}
			if (e.name === "return" || e.name === "enter") {
				e.preventDefault();
				if (e.ctrl) {
					beginRangeSelection();
					return;
				}
				confirmSelectedLineComment();
				return;
			}
			if (e.name === "f") {
				e.preventDefault();
				const file = selectedFile();
				if (!file) return;
				void openFileNoteEditor(file);
				return;
			}
			if (e.name === "x") {
				e.preventDefault();
				clearOrCancelLineSelection();
				return;
			}
			if (e.name === "s") {
				e.preventDefault();
				submitReview();
			}
			return;
		}

		if (e.name === "escape") {
			e.preventDefault();
			props.onClose();
			return;
		}
		if (e.name === "up" || e.name === "k") {
			e.preventDefault();
			setSelectedIndex((index) => Math.max(0, index - 1));
			return;
		}
		if (e.name === "down" || e.name === "j") {
			e.preventDefault();
			setSelectedIndex((index) =>
				Math.min(reviewFiles().length - 1, index + 1),
			);
			return;
		}
		if (e.name === "return" || e.name === "enter") {
			e.preventDefault();
			const file = selectedFile();
			if (file && expandedKeys().has(file.id)) {
				setMode("patch");
				if (file.hunks.length > 0) {
					setSelectedHunkIndex(
						file.id,
						selectedHunkIndices().get(file.id) ?? 0,
					);
				}
			}
			return;
		}
		if (e.name === "space") {
			e.preventDefault();
			const file = selectedFile();
			if (file) toggleExpanded(file.id);
			return;
		}
		if (e.name === "f") {
			e.preventDefault();
			const file = selectedFile();
			if (!file) return;
			void openFileNoteEditor(file);
			return;
		}
		if (e.name === "x") {
			e.preventDefault();
			clearSelectedFileNote();
			return;
		}
		if (e.name === "s") {
			e.preventDefault();
			submitReview();
		}
	});

	return (
		<ScreenLayout
			surfaceProps={props.surfaceProps}
			zIndex={1200}
			header={
				<ScreenHeader
					left={<text fg={theme.textMuted}>Review diff</text>}
					right={
						<text fg={theme.textMuted}>
							{reviewFiles().length} file{reviewFiles().length === 1 ? "" : "s"}
							{totalDraftNotes() > 0
								? ` · ${formatNoteCount(totalDraftNotes())}`
								: ""}
						</text>
					}
				/>
			}
			footer={<HintBar bindings={FOCUS_BINDINGS[mode()]} />}
		>
			<Show
				when={!files.loading}
				fallback={
					<box flexGrow={1} padding={1}>
						<text fg={theme.textMuted}>Loading diff…</text>
					</box>
				}
			>
				<Show
					when={reviewFiles().length > 0}
					fallback={
						<box flexGrow={1} padding={1}>
							<text fg={theme.textMuted}>No uncommitted changes.</text>
						</box>
					}
				>
					<Show
						when={mode() !== "list" && selectedFile()}
						fallback={
							<scrollbox
								ref={(value) => {
									listScrollRef = value as ScrollRef;
								}}
								flexGrow={1}
								scrollY
								padding={1}
							>
								<box flexDirection="column" gap={0}>
									<For each={reviewFiles()}>
										{(file, idx) => {
											const selected = () => idx() === selectedIndex();
											const expanded = () => expandedKeys().has(file.id);
											const noteCount = () =>
												countFileDraftNotes(file, draftState());
											return (
												<box
													id={`review-file-${file.id}`}
													flexDirection="column"
													gap={0}
													backgroundColor={
														selected() ? theme.bgMuted : theme.bgTransparent
													}
												>
													<box
														paddingX={1}
														paddingY={0}
														flexDirection="row"
														justifyContent="space-between"
													>
														<box flexDirection="column">
															<text
																fg={
																	selected()
																		? theme.textPrimary
																		: theme.textSecondary
																}
															>
																{expanded() ? "▾" : "▸"} {statusLabel(file)}{" "}
																{file.path}
																{noteCount() > 0
																	? ` · ✎ ${formatNoteCount(noteCount())}`
																	: ""}
															</text>
															<Show when={file.prevPath}>
																<text fg={theme.textMuted}>
																	from {file.prevPath}
																</text>
															</Show>
														</box>
														<text fg={theme.textMuted}>
															{file.hunks.length} hunk
															{file.hunks.length === 1 ? "" : "s"} ·{" "}
															{file.changeCount} changed line
															{file.changeCount === 1 ? "" : "s"}
														</text>
													</box>
													<Show when={expanded()}>
														<box
															padding={1}
															paddingTop={0}
															flexDirection="column"
															gap={0}
														>
															<diff
																diff={file.rawPatch}
																view="unified"
																filetype={file.filetype}
																syntaxStyle={syntaxStyle()}
																showLineNumbers
																addedBg={theme.diffAddedBg}
																removedBg={theme.diffRemovedBg}
																contextBg={theme.bgSurface}
																addedContentBg={theme.diffAddedContentBg}
																removedContentBg={theme.diffRemovedContentBg}
																contextContentBg={theme.bgSurface}
																addedSignColor={theme.toolText}
																removedSignColor={theme.errorText}
																lineNumberFg={theme.textMuted}
																lineNumberBg={theme.bg}
																addedLineNumberBg={theme.diffAddedLineNumberBg}
																removedLineNumberBg={
																	theme.diffRemovedLineNumberBg
																}
																wrapMode="none"
															/>
														</box>
													</Show>
												</box>
											);
										}}
									</For>
								</box>
							</scrollbox>
						}
					>
						{(file) => {
							const currentHunk = createMemo(() => selectedHunk());
							const fileNote = createMemo(() => selectedFileNote(file()));
							return (
								<box
									flexGrow={1}
									flexDirection="column"
									gap={1}
									backgroundColor={theme.bgMuted}
								>
									<box
										flexShrink={0}
										paddingX={1}
										paddingY={0}
										flexDirection="row"
										justifyContent="space-between"
									>
										<box flexDirection="column">
											<text fg={theme.textPrimary}>
												{statusLabel(file())} {file().path}
											</text>
											<Show when={file().prevPath}>
												<text fg={theme.textMuted}>from {file().prevPath}</text>
											</Show>
											<Show when={activeLineStatus().length > 0}>
												<text fg={theme.textMuted}>{activeLineStatus()}</text>
											</Show>
										</box>
										<text fg={theme.textMuted}>
											{currentHunk()
												? `change group ${selectedHunkIndex() + 1}/${file().hunks.length}`
												: `${file().hunks.length} change group${file().hunks.length === 1 ? "" : "s"}`}
										</text>
									</box>

									<Show
										when={
											fileNote().length > 0 || selectedRangeNote().length > 0
										}
									>
										<box
											flexShrink={0}
											marginX={1}
											border
											borderColor={theme.borderDefault}
											paddingX={1}
											flexDirection="column"
											gap={0}
										>
											<Show when={fileNote().length > 0}>
												<text fg={theme.textPrimary}>
													File note: {fileNote()}
												</text>
											</Show>
											<Show when={selectedRangeNote().length > 0}>
												<text fg={theme.textPrimary}>
													{currentLineNoteLabel()}: {selectedRangeNote()}
												</text>
											</Show>
										</box>
									</Show>

									<box
										flexGrow={1}
										padding={1}
										paddingTop={0}
										backgroundColor={theme.bg}
									>
										<scrollbox
											ref={(value) => {
												if (value)
													patchScrollRefs.set(
														file().id,
														value as PatchScrollRef,
													);
											}}
											flexGrow={1}
											scrollY
										>
											<box position="relative" paddingLeft={1}>
												<diff
													diff={file().rawPatch}
													view="unified"
													filetype={file().filetype}
													syntaxStyle={syntaxStyle()}
													showLineNumbers
													addedBg={theme.diffAddedBg}
													removedBg={theme.diffRemovedBg}
													contextBg={theme.bgSurface}
													addedContentBg={theme.diffAddedContentBg}
													removedContentBg={theme.diffRemovedContentBg}
													contextContentBg={theme.bgSurface}
													addedSignColor={theme.toolText}
													removedSignColor={theme.errorText}
													lineNumberFg={theme.textMuted}
													lineNumberBg={theme.bg}
													addedLineNumberBg={theme.diffAddedLineNumberBg}
													removedLineNumberBg={theme.diffRemovedLineNumberBg}
													wrapMode="none"
												/>
												<Show when={lineCursorState()}>
													{(cursor) => (
														<>
															<Show when={activeRangeLineBounds()}>
																{(bounds) => (
																	<box
																		position="absolute"
																		left={0}
																		top={
																			cursor().hunk.patchStartLine +
																			bounds().startIndex
																		}
																		height={
																			bounds().endIndex -
																			bounds().startIndex +
																			1
																		}
																		width={1}
																	>
																		<text fg={theme.borderAccent}>
																			{buildRangeMarker(
																				bounds().endIndex -
																					bounds().startIndex +
																					1,
																			)}
																		</text>
																	</box>
																)}
															</Show>
															<Show when={anchorLineIndex() !== null}>
																<box
																	position="absolute"
																	left={0}
																	top={
																		cursor().hunk.patchStartLine +
																		(anchorLineIndex() ?? 0)
																	}
																	height={1}
																	width={1}
																>
																	<text fg={theme.borderFocused}>◆</text>
																</box>
															</Show>
															<box
																id={`review-line-cursor-${cursor().hunk.id}-${cursor().line.index}`}
																position="absolute"
																left={0}
																top={
																	cursor().hunk.patchStartLine +
																	cursor().line.index
																}
																height={1}
																width={1}
															>
																<text fg={theme.borderAccent}>▎</text>
															</box>
														</>
													)}
												</Show>
											</box>
										</scrollbox>
									</box>
								</box>
							);
						}}
					</Show>
				</Show>
			</Show>
		</ScreenLayout>
	);
}
