import { readFileSync } from "node:fs";
import path from "node:path";
import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import type { DiffLineAnnotation } from "@pierre/diffs";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	For,
	onCleanup,
	Show,
} from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { ReviewDiffView } from "../../settings";
import type { AttachmentsController } from "../../shell/attachments-controller";
import { Dialog } from "../../shell/Dialog";
import {
	estimateWrappedRows,
	getReviewDiffActiveLineId,
	getReviewDiffCommentableLines,
	getReviewDiffLineTop,
	getReviewDiffRangeBounds,
	type ReviewDiffAnnotationMetadata,
	ReviewDiffBlock,
	type ReviewDiffCommentableLine,
	shouldResetPatchScroll,
} from "../../shell/diff/ReviewDiffBlock";
import { inferFiletype } from "../../shell/filetype";
import {
	DASHED_VERTICAL,
	DIAMOND,
	TRIANGLE_DOWN,
	TRIANGLE_RIGHT,
} from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { MessageComposer, type TextareaRef } from "../../shell/MessageComposer";
import { Picker } from "../../shell/Picker";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { syntaxStyle, theme } from "../../shell/theme";
import type { PickerOption } from "../../state/picker";
import { createPickerManager } from "../../state/picker-manager";
import type { ToastInput } from "../../state/toasts";
import { CodeReviewAttachment } from "./attachment";
import {
	buildRangeNoteKey,
	buildReviewSubmission,
	countDraftNotes,
	parseRangeNoteKey,
	type ReviewDraftState,
	type ReviewRangeDraft,
} from "./draft";
import { FileTreePanel } from "./FileTreePanel";
import {
	getRepoRoot,
	listRepoFiles,
	loadReviewFiles,
	type ReviewFile,
	type ReviewHunk,
	type ReviewSkippedSection,
} from "./model";
import {
	reviewStatusColor,
	reviewStatusLabel,
	reviewStatusText,
} from "./status";

export type ReviewContentProps = {
	onClose: () => void;
	attachments: AttachmentsController;
	toast: (toast: ToastInput) => void;
	defaultDiffView: ReviewDiffView;
	onDiffViewChanged?: (view: ReviewDiffView) => void;
	surfaceProps?: OverlayComponentProps<void>["surfaceProps"];
};

type ReviewMode = "tree" | "patch";
type ReviewSide = "additions" | "deletions";
type CommentableLine = ReviewDiffCommentableLine;

const WIDE_VIEWPORT_THRESHOLD = 100;
const TREE_PANEL_WIDTH = 36;
const MIN_TREE_PANEL_WIDTH = 28;

const PATCH_FOCUSED_RENDER_HUNK_LIMIT = 40;
const PATCH_FOCUSED_RENDER_LINE_LIMIT = 800;
const PATCH_WINDOW_HUNK_LIMIT = 12;
const PATCH_WINDOW_LINE_LIMIT = 800;

type RangeAnchor = {
	side: ReviewSide;
	lineNumber: number;
};

type PatchScrollRef = {
	scrollBy: (delta: number | { x: number; y: number }) => void;
	scrollChildIntoView?: (childId: string) => void;
	scrollTop?: number;
	scrollTo?: (position: number | { x: number; y: number }) => void;
};

function formatNoteCount(count: number): string {
	return `${count} note${count === 1 ? "" : "s"}`;
}

function formatFileCount(count: number): string {
	return `${count} file${count === 1 ? "" : "s"}`;
}

function getReviewFileRenderedLineCount(file: ReviewFile): number {
	if (file.hunks.length === 0) {
		return file.rawPatch.replace(/\r\n/g, "\n").split("\n").length;
	}
	return file.unifiedLineCount;
}

function shouldUseFocusedPatchRendering(file: ReviewFile): boolean {
	return (
		file.hunks.length > PATCH_FOCUSED_RENDER_HUNK_LIMIT ||
		getReviewFileRenderedLineCount(file) > PATCH_FOCUSED_RENDER_LINE_LIMIT
	);
}

/**
 * Short label for non-default sources. Working-tree changes are the
 * dominant case and don't need a redundant prefix; only untracked files
 * get a label.
 */
function sourceLabel(file: ReviewFile): string {
	switch (file.source) {
		case "working":
			return "";
		case "untracked":
			return "untracked";
	}
}

function formatSkippedSectionSummary(
	sectionCount: number,
	lineCount: number,
): string {
	if (sectionCount === 0 || lineCount === 0) return "";
	return `${sectionCount} skipped section${sectionCount === 1 ? "" : "s"} · ${lineCount} unchanged line${lineCount === 1 ? "" : "s"}`;
}

function getSkippedSection(
	file: ReviewFile,
	beforeHunkIndex: number,
): ReviewSkippedSection | undefined {
	return file.skippedSections.find(
		(section) => section.beforeHunkIndex === beforeHunkIndex,
	);
}

function skippedSectionLineLabel(section: ReviewSkippedSection): string {
	const start =
		section.additionStart > 0 ? section.additionStart : section.deletionStart;
	const end = start + section.lineCount - 1;
	return start === end ? `line ${start}` : `lines ${start}-${end}`;
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
	diffView: ReviewDiffView = "unified",
): CommentableLine[] {
	return getReviewDiffCommentableLines(hunk, side, diffView);
}

function getCommentableLineTop(
	hunk: ReviewHunk,
	lineIndex: number,
	diffView: ReviewDiffView,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] = [],
	contentColumns?: number,
): number {
	return getReviewDiffLineTop(
		hunk,
		lineIndex,
		diffView,
		annotations,
		contentColumns,
	);
}

function getVisualBoundsForRange(
	hunk: ReviewHunk,
	range: ReviewRangeDraft,
	diffView: ReviewDiffView,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] = [],
	contentColumns?: number,
) {
	return getReviewDiffRangeBounds(
		hunk,
		range,
		diffView,
		annotations,
		contentColumns,
	);
}

/** Line-number column width for a hunk's gutter. */
function lineNumberWidthForHunk(hunk: ReviewHunk): number {
	const maxLineNumber = Math.max(
		0,
		...hunk.lines.flatMap((line) => [
			line.deletionLineNumber ?? 0,
			line.additionLineNumber ?? 0,
		]),
	);
	return Math.max(1, String(maxLineNumber).length);
}

// Hunk content chrome widths (matches the layout in renderHunkBlock):
//   patch box has padding={1} (left + right = 2)
//   each hunk wrapper has paddingLeft={2}
const PATCH_CONTENT_PADDING = 2;
const HUNK_PADDING_LEFT = 2;

function unifiedContentColumns(
	hunk: ReviewHunk,
	diffPaneWidth: number,
): number {
	const lnw = lineNumberWidthForHunk(hunk);
	// Unified row: [lnw][space][lnw][sign][space]
	const gutterCols = 2 * lnw + 3;
	return Math.max(
		10,
		diffPaneWidth - PATCH_CONTENT_PADDING - HUNK_PADDING_LEFT - gutterCols,
	);
}

function splitContentColumns(hunk: ReviewHunk, diffPaneWidth: number): number {
	const lnw = lineNumberWidthForHunk(hunk);
	const inner = diffPaneWidth - PATCH_CONTENT_PADDING - HUNK_PADDING_LEFT;
	const halfWidth = Math.floor(inner / 2);
	// Split cell: [lnw][sign][space]
	return Math.max(10, halfWidth - lnw - 2);
}

function contentColumnsFor(
	hunk: ReviewHunk,
	view: ReviewDiffView,
	diffPaneWidth: number,
): number {
	return view === "split"
		? splitContentColumns(hunk, diffPaneWidth)
		: unifiedContentColumns(hunk, diffPaneWidth);
}

function lineRangeLabel(range: ReviewRangeDraft): string {
	const startLine = Math.min(range.startLine, range.endLine);
	const endLine = Math.max(range.startLine, range.endLine);
	return startLine === endLine
		? `${range.side} ${startLine}`
		: `${range.side} ${startLine}-${endLine}`;
}

function buildRangeMarker(height: number): string {
	return Array.from(
		{ length: Math.max(1, height) },
		() => DASHED_VERTICAL,
	).join("\n");
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

function rangeToAnnotation(
	range: ReviewRangeDraft,
	comment: string,
	options?: { editing?: boolean },
): DiffLineAnnotation<ReviewDiffAnnotationMetadata> {
	const startLine = Math.min(range.startLine, range.endLine);
	const endLine = Math.max(range.startLine, range.endLine);
	return {
		side: range.side,
		lineNumber: endLine,
		metadata: {
			key: buildRangeNoteKey(range),
			comment,
			side: range.side,
			startLine,
			endLine,
			...(options?.editing ? { editing: true } : {}),
		},
	};
}

function buildSavedCommentAnnotations(
	path: string,
	rangeNotes: Map<string, string>,
): DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] {
	const annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] = [];
	for (const [key, value] of rangeNotes) {
		if (!value.trim()) continue;
		const range = parseRangeNoteKey(key);
		if (!range || range.path !== path) continue;
		annotations.push(rangeToAnnotation(range, value.trim()));
	}
	return annotations;
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
	const repoRoot = getRepoRoot();
	const [files] = createResource(() => loadReviewFiles());
	const [allFiles] = createResource(() => listRepoFiles());
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [mode, setMode] = createSignal<ReviewMode>("tree");
	const [contentWidth, setContentWidth] = createSignal(120);
	const [treeFocusedPath, setTreeFocusedPath] = createSignal<string | null>(
		null,
	);
	const [fileFinderOpen, setFileFinderOpen] = createSignal(false);
	const [viewingFilePath, setViewingFilePath] = createSignal<string | null>(
		null,
	);
	const [viewingFileLine, setViewingFileLine] = createSignal(1);
	const [viewingFileLineCount, setViewingFileLineCount] = createSignal(0);
	const [viewingFileEditingRange, setViewingFileEditingRange] =
		createSignal<ReviewRangeDraft | null>(null);
	const [viewingFileEditingValue, setViewingFileEditingValue] =
		createSignal("");
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
	const [selectedSectionIds, setSelectedSectionIds] = createSignal<
		Map<string, string>
	>(new Map());
	const [expandedSectionIds, setExpandedSectionIds] = createSignal<Set<string>>(
		new Set(),
	);
	const [diffView, setDiffView] = createSignal<ReviewDiffView>(
		props.defaultDiffView,
	);
	const [rangeAnchor, setRangeAnchor] = createSignal<RangeAnchor | null>(null);
	const [editingRange, setEditingRange] = createSignal<ReviewRangeDraft | null>(
		null,
	);
	const [editingRangeValue, setEditingRangeValue] = createSignal("");
	const [editingFileNoteKey, setEditingFileNoteKey] = createSignal<
		string | null
	>(null);
	const [editingFileNoteValue, setEditingFileNoteValue] = createSignal("");
	const [editorOpen, setEditorOpen] = createSignal(false);
	// The diff pane has a single scrollbox shared across files (the
	// <Show when={selectedFile()}> is non-keyed). Keep a single ref —
	// a per-file map would only ever capture the initial file's id because
	// ref callbacks don't refire on reactive prop updates.
	let patchScrollRef: PatchScrollRef | undefined;
	let lastPatchFileId: string | undefined;
	let pendingPatchOpenReset = false;
	let pendingPatchScrollReset = false;

	function applyPendingPatchScrollReset(ref: PatchScrollRef): boolean {
		if (!pendingPatchScrollReset) return false;
		if (ref.scrollTo) ref.scrollTo(0);
		else if (typeof ref.scrollTop === "number") ref.scrollTop = 0;
		pendingPatchScrollReset = false;
		pendingPatchOpenReset = false;
		return true;
	}

	let contentRef: { width: number } | undefined;

	const isWide = createMemo(() => contentWidth() >= WIDE_VIEWPORT_THRESHOLD);
	const treePanelWidth = createMemo(() =>
		Math.max(
			MIN_TREE_PANEL_WIDTH,
			Math.min(TREE_PANEL_WIDTH, Math.floor(contentWidth() * 0.35)),
		),
	);
	const diffPaneWidth = createMemo(() =>
		isWide() ? Math.max(0, contentWidth() - treePanelWidth()) : contentWidth(),
	);

	let patchCursorScrollTimeout: ReturnType<typeof setTimeout> | undefined;

	const reviewFiles = createMemo(() => files() ?? []);
	const draftState = createMemo<ReviewDraftState>(() => ({
		fileNotes: fileNotes(),
		rangeNotes: rangeNotes(),
	}));
	const totalDraftNotes = createMemo(() => countDraftNotes(draftState()));
	const reviewFilesByPath = createMemo(() => {
		const map = new Map<string, ReviewFile>();
		for (const file of reviewFiles()) {
			map.set(file.path, file);
		}
		return map;
	});
	const selectedFile = createMemo(() => {
		// Viewing an unchanged file — no ReviewFile to show
		if (viewingFilePath()) return null;
		// In tree mode, use the tree-focused path
		const focused = treeFocusedPath();
		if (focused) {
			const byPath = reviewFilesByPath().get(focused);
			if (byPath) return byPath;
		}
		// Fallback to index-based selection
		return reviewFiles()[selectedIndex()] ?? null;
	});
	const fileFinderOptions = createMemo<PickerOption[]>(() => {
		const paths = Array.from(
			new Set([
				...(allFiles() ?? []),
				...reviewFiles().map((file) => file.path),
			]),
		);
		const byPath = reviewFilesByPath();
		return paths.map((filePath) => {
			const file = byPath.get(filePath);
			return {
				name: filePath,
				description: file ? reviewStatusText(file) : "",
				nameColor: file ? reviewStatusColor(file) : undefined,
				action: (ctx) => {
					ctx.dismiss();
					selectFilePath(filePath);
				},
			};
		});
	});
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
	const selectedSkippedSection = createMemo(() => {
		const file = selectedFile();
		if (!file) return null;
		const sectionId = selectedSectionIds().get(file.id);
		if (!sectionId) return null;
		return (
			file.skippedSections.find((section) => section.id === sectionId) ?? null
		);
	});
	const selectedCommentableLines = createMemo(() => {
		const hunk = selectedHunk();
		if (!hunk) return [];
		return getCommentableLines(hunk, rangeAnchor()?.side, diffView());
	});
	const selectedLine = createMemo(() => {
		if (selectedSkippedSection()) return null;
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
	const activeLineStatus = createMemo(() => {
		const range = selectedRange();
		if (!range || mode() !== "patch" || !rangeAnchor()) return "";
		return `Selecting ${lineRangeLabel(range)} · press Enter to comment`;
	});
	const hiddenContextStatus = createMemo(() => {
		const file = selectedFile();
		if (!file || mode() !== "patch") return "";
		const selectedSection = selectedSkippedSection();
		if (selectedSection) {
			const expanded = expandedSectionIds().has(selectedSection.id);
			return `${skippedSectionLineLabel(selectedSection)} · ${selectedSection.lineCount} unchanged line${selectedSection.lineCount === 1 ? "" : "s"} ${expanded ? "shown" : "hidden"} · press Space to ${expanded ? "collapse" : "expand"}`;
		}
		const hiddenSections = file.skippedSections.filter(
			(section) => !expandedSectionIds().has(section.id),
		);
		const summary = formatSkippedSectionSummary(
			hiddenSections.length,
			hiddenSections.reduce((sum, section) => sum + section.lineCount, 0),
		);
		if (summary.length === 0) {
			return file.skippedSections.length > 0
				? "All skipped sections shown · use ↑/↓ to select one"
				: "";
		}
		return `${summary} hidden · use ↑/↓ to select one`;
	});
	const lineCursorState = createMemo(() => {
		const hunk = selectedHunk();
		const line = selectedLine();
		if (mode() !== "patch" || !hunk || !line) return null;
		return { hunk, line };
	});
	const selectedFileCommentAnnotations = createMemo(() => {
		const file = selectedFile();
		if (!file) return [];
		const editing = editingRange();
		const editingKey = editing ? buildRangeNoteKey(editing) : null;
		const saved = buildSavedCommentAnnotations(file.path, rangeNotes()).filter(
			(annotation) => annotation.metadata.key !== editingKey,
		);
		if (!editing || editing.path !== file.path) return saved;
		return [
			...saved,
			rangeToAnnotation(editing, editingRangeValue(), { editing: true }),
		];
	});
	const anchorLineTop = createMemo(() => {
		const anchor = rangeAnchor();
		const hunk = selectedHunk();
		if (!anchor || !hunk) return null;
		const line = getCommentableLines(hunk, anchor.side, diffView()).find(
			(candidate) => candidate.lineNumber === anchor.lineNumber,
		);
		return line
			? getCommentableLineTop(
					hunk,
					line.index,
					diffView(),
					selectedFileCommentAnnotations(),
					contentColumnsFor(hunk, diffView(), diffPaneWidth()),
				)
			: null;
	});
	const activeRangeLineBounds = createMemo(() => {
		const range = selectedRange();
		const anchor = rangeAnchor();
		const hunk = selectedHunk();
		if (!range || !anchor || !hunk) return null;
		return getVisualBoundsForRange(
			hunk,
			range,
			diffView(),
			selectedFileCommentAnnotations(),
			contentColumnsFor(hunk, diffView(), diffPaneWidth()),
		);
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
			setSelectedSectionIds(new Map<string, string>());
			setExpandedSectionIds(new Set<string>());
			setMode("tree");
			setRangeAnchor(null);
		}
	});

	createEffect(() => {
		const file = selectedFile();
		if (!file) return;
		const sectionId = selectedSectionIds().get(file.id);
		if (
			sectionId &&
			!file.skippedSections.some((section) => section.id === sectionId)
		) {
			setSelectedSectionId(file.id, null);
		}
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
		clearPatchCursorScrollTimeout();
		if (mode() !== "patch") return;
		const file = selectedFile();
		if (!file) return;

		const section = selectedSkippedSection();
		const hunk = selectedHunk();
		const line = selectedLine();
		const childId = section
			? `review-skipped-section-${section.id}`
			: hunk && line
				? getReviewDiffActiveLineId(hunk.id, line.index)
				: null;
		if (
			shouldResetPatchScroll(lastPatchFileId, file.id, pendingPatchOpenReset)
		) {
			pendingPatchScrollReset = true;
		}
		lastPatchFileId = file.id;
		if (!childId && !pendingPatchScrollReset) return;
		patchCursorScrollTimeout = setTimeout(() => {
			patchCursorScrollTimeout = undefined;
			const ref = patchScrollRef;
			if (!ref) return;
			// On file switch/open, reset to top first so the new file doesn't
			// inherit the previous file's scroll offset. Clear pending reset flags
			// only after a ref exists and the reset has actually been applied.
			applyPendingPatchScrollReset(ref);
			if (childId) ref.scrollChildIntoView?.(childId);
		}, 0);
		onCleanup(clearPatchCursorScrollTimeout);
	});

	function clearPatchCursorScrollTimeout() {
		if (!patchCursorScrollTimeout) return;
		clearTimeout(patchCursorScrollTimeout);
		patchCursorScrollTimeout = undefined;
	}

	function selectedFileNote(file: ReviewFile): string {
		return fileNotes().get(file.noteKey)?.trim() ?? "";
	}

	function setSelectedSectionId(fileId: string, sectionId: string | null) {
		setSelectedSectionIds((prev) => {
			const next = new Map(prev);
			if (sectionId) next.set(fileId, sectionId);
			else next.delete(fileId);
			return next;
		});
	}

	function setActiveHunkIndex(fileId: string, index: number) {
		setSelectedHunkIndices((prev) => {
			const next = new Map(prev);
			next.set(fileId, index);
			return next;
		});
	}

	function setSelectedHunkIndex(fileId: string, index: number) {
		setActiveHunkIndex(fileId, index);
		setSelectedSectionId(fileId, null);
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

	function focusSkippedSection(
		file: ReviewFile,
		section: ReviewSkippedSection,
	) {
		setSelectedSectionId(file.id, section.id);
		if (file.hunks.length > 0) {
			setActiveHunkIndex(
				file.id,
				Math.max(0, Math.min(file.hunks.length - 1, section.beforeHunkIndex)),
			);
		}
	}

	function focusHunkLine(
		file: ReviewFile,
		hunkIndex: number,
		lineIndex: number,
	) {
		const hunk = file.hunks[hunkIndex];
		if (!hunk) return;
		setActiveHunkIndex(file.id, hunkIndex);
		setSelectedSectionId(file.id, null);
		setSelectedLineIndex(hunk.id, lineIndex);
	}

	function findAdjacentHunkWithLines(
		file: ReviewFile,
		startIndex: number,
		direction: 1 | -1,
	) {
		for (
			let hunkIndex = startIndex;
			hunkIndex >= 0 && hunkIndex < file.hunks.length;
			hunkIndex += direction
		) {
			const hunk = file.hunks[hunkIndex];
			const lines = getCommentableLines(hunk, undefined, diffView());
			if (lines.length === 0) continue;
			return { hunkIndex, lines };
		}
		return null;
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
		if (!file) return;

		const selectedSection = selectedSkippedSection();
		if (selectedSection) {
			const direction: 1 | -1 = delta > 0 ? 1 : -1;
			const adjacent = findAdjacentHunkWithLines(
				file,
				direction > 0
					? selectedSection.beforeHunkIndex
					: selectedSection.beforeHunkIndex - 1,
				direction,
			);
			if (!adjacent) return;
			focusHunkLine(
				file,
				adjacent.hunkIndex,
				direction > 0 ? 0 : adjacent.lines.length - 1,
			);
			return;
		}

		const hunk = selectedHunk();
		const lines = selectedCommentableLines();
		if (!hunk || lines.length === 0) return;
		const currentLine = selectedLine();
		const current = currentLine ? lines.indexOf(currentLine) : -1;
		const nextIndex = (current >= 0 ? current : 0) + delta;
		if (nextIndex >= 0 && nextIndex < lines.length) {
			setSelectedLineIndex(hunk.id, nextIndex);
			return;
		}

		if (rangeAnchor()) return;

		const direction: 1 | -1 = delta > 0 ? 1 : -1;
		const boundarySection = getSkippedSection(
			file,
			direction > 0 ? selectedHunkIndex() + 1 : selectedHunkIndex(),
		);
		if (boundarySection) {
			focusSkippedSection(file, boundarySection);
			return;
		}

		const adjacent = findAdjacentHunkWithLines(
			file,
			selectedHunkIndex() + direction,
			direction,
		);
		if (!adjacent) return;
		focusHunkLine(
			file,
			adjacent.hunkIndex,
			direction > 0 ? 0 : adjacent.lines.length - 1,
		);
	}

	function toggleExpandedContext(sectionId: string) {
		setExpandedSectionIds((prev) => {
			const next = new Set(prev);
			if (next.has(sectionId)) next.delete(sectionId);
			else next.add(sectionId);
			return next;
		});
	}

	function toggleDiffView() {
		const hunk = selectedHunk();
		const line = selectedLine();
		const nextView = diffView() === "unified" ? "split" : "unified";
		setDiffView(nextView);
		props.onDiffViewChanged?.(nextView);
		if (!hunk || !line) return;
		const nextLines = getCommentableLines(hunk, rangeAnchor()?.side, nextView);
		const nextIndex = nextLines.findIndex(
			(candidate) => candidate.index === line.index,
		);
		if (nextIndex >= 0) {
			setSelectedLineIndex(hunk.id, nextIndex);
		}
	}

	function selectFilePath(filePath: string) {
		const file = reviewFilesByPath().get(filePath);
		if (file) {
			const idx = reviewFiles().indexOf(file);
			if (idx >= 0) setSelectedIndex(idx);
			setViewingFilePath(null);
			pendingPatchOpenReset = true;
			setMode("patch");
			if (file.hunks.length > 0) {
				// Opening a file from the tree should start at the top of that file,
				// not restore a previous hunk/line or inherit another file's viewport.
				setSelectedHunkIndex(file.id, 0);
			}
		} else {
			setViewingFilePath(filePath);
			setViewingFileLine(1);
			setMode("patch");
		}
	}

	function renderRawDiffBlock(rawPatch: string, filetype?: string) {
		return (
			<ReviewDiffBlock
				rawPatch={rawPatch}
				view={diffView()}
				filetype={filetype}
			/>
		);
	}

	function renderSkippedSectionRow(
		section: ReviewSkippedSection,
		options: {
			interactive: boolean;
			selected: () => boolean;
			expanded: () => boolean;
		},
	) {
		return (
			<box
				id={`review-skipped-section-${section.id}`}
				paddingX={1}
				paddingY={0}
				flexDirection="row"
				justifyContent="space-between"
				backgroundColor={options.selected() ? theme.bgMuted : theme.bgSurface}
			>
				<text fg={options.selected() ? theme.metaText : theme.textMuted}>
					{options.expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}{" "}
					{section.lineCount} unchanged line
					{section.lineCount === 1 ? "" : "s"}{" "}
					{options.expanded() ? "shown" : "hidden"}
				</text>
				<text
					fg={options.selected() ? theme.textSecondary : theme.textPlaceholder}
				>
					{skippedSectionLineLabel(section)}
					{options.interactive && options.selected()
						? ` · Space ${options.expanded() ? "collapse" : "expand"}`
						: ""}
				</text>
			</box>
		);
	}

	function renderFileNoteBlock(file: ReviewFile) {
		const editing = () => editingFileNoteKey() === file.noteKey;
		const note = () => selectedFileNote(file);
		let textareaRef: TextareaRef | undefined;
		return (
			<Show when={editing() || note().length > 0}>
				<Show
					when={editing()}
					fallback={
						<box
							border
							borderColor={theme.borderDefault}
							backgroundColor={theme.bgSurface}
							paddingX={1}
							flexShrink={0}
						>
							<text fg={theme.textPrimary} bg={theme.bgSurface}>
								{note()}
							</text>
						</box>
					}
				>
					<MessageComposer
						ref={(value) => {
							textareaRef = value;
						}}
						initialValue={editingFileNoteValue()}
						placeholder="Comment on the whole file..."
						backgroundColor={theme.bgTransparent}
						focusedBackgroundColor={theme.bgTransparent}
						keyBindings={[
							{ name: "return", action: "submit" },
							{ name: "return", shift: true, action: "newline" },
						]}
						onContentChange={() =>
							setEditingFileNoteValue(textareaRef?.plainText ?? "")
						}
						onSubmit={saveFileNoteEditor}
					/>
				</Show>
			</Show>
		);
	}

	function renderHunkBlock(
		file: ReviewFile,
		hunk: ReviewHunk,
		interactive: boolean,
	) {
		const annotations = () =>
			interactive ? selectedFileCommentAnnotations() : [];
		const cursor = () => lineCursorState();
		const cursorTop = () => {
			const current = cursor();
			if (!interactive || current?.hunk.id !== hunk.id) return null;
			return getCommentableLineTop(
				hunk,
				current.line.index,
				diffView(),
				annotations(),
				contentColumnsFor(hunk, diffView(), diffPaneWidth()),
			);
		};
		const cursorSide = () => {
			const current = cursor();
			if (!interactive || current?.hunk.id !== hunk.id) return null;
			return current.line.side;
		};
		const rangeBounds = () => activeRangeLineBounds();
		const anchorTop = () => anchorLineTop();
		const splitView = () => diffView() === "split";
		const activeLine = () => {
			const current = cursor();
			if (!interactive || current?.hunk.id !== hunk.id) return undefined;
			return current.line;
		};
		const renderOverlayLane = (side?: ReviewSide) => (
			<>
				<Show when={cursorTop() !== null && (!side || cursorSide() === side)}>
					<Show when={rangeBounds()}>
						{(bounds) => (
							<box
								position="absolute"
								left={0}
								top={bounds().top}
								height={bounds().height}
								width={1}
							>
								<text fg={theme.borderAccent}>
									{buildRangeMarker(bounds().height)}
								</text>
							</box>
						)}
					</Show>
					<Show
						when={
							anchorTop() !== null && (!side || rangeAnchor()?.side === side)
						}
					>
						<box
							position="absolute"
							left={0}
							top={anchorTop() ?? 0}
							height={1}
							width={1}
						>
							<text fg={theme.borderFocused}>{DIAMOND}</text>
						</box>
					</Show>
				</Show>
			</>
		);
		return (
			<box flexDirection="column" gap={0}>
				<box
					paddingLeft={2}
					paddingX={1}
					backgroundColor={theme.bgMuted}
					height={1}
					flexShrink={0}
				>
					<text fg={theme.metaText} bg={theme.bgMuted}>
						{hunk.header}
						{hunk.context ? ` ${hunk.context}` : ""}
					</text>
				</box>
				<box position="relative" paddingLeft={2}>
					<ReviewDiffBlock
						hunk={hunk}
						view={diffView()}
						filetype={file.filetype}
						annotations={annotations()}
						activeLine={activeLine()}
						contentColumns={contentColumnsFor(
							hunk,
							diffView(),
							diffPaneWidth(),
						)}
						annotationEditor={
							editingRange()
								? {
										onChange: setEditingRangeValue,
										onSubmit: saveRangeNoteEditor,
									}
								: undefined
						}
						onLineMouseDown={
							interactive
								? (line, event) =>
										handleDiffLineMouseDown(file, hunk, line, event)
								: undefined
						}
					/>
					<Show when={interactive}>
						<Show
							when={splitView()}
							fallback={
								<box position="absolute" left={0} top={0}>
									{renderOverlayLane()}
								</box>
							}
						>
							<box position="absolute" left={0} top={0} width="50%">
								{renderOverlayLane("deletions")}
							</box>
							<box position="absolute" left="50%" top={0} width="50%">
								{renderOverlayLane("additions")}
							</box>
						</Show>
					</Show>
				</box>
			</box>
		);
	}

	function getRenderableLineCount(hunk: ReviewHunk): number {
		return hunk.lines.length;
	}

	function getPatchWindowHunk(
		hunk: ReviewHunk,
		forceWindow: boolean,
	): ReviewHunk {
		if (!forceWindow || hunk.patchLineCount <= PATCH_WINDOW_LINE_LIMIT)
			return hunk;
		const sourceOffset = hunk.lineIndexOffset ?? 0;
		const selectedLineIndex =
			selectedLineIndices().get(hunk.id) ?? sourceOffset;
		const halfWindow = Math.floor(PATCH_WINDOW_LINE_LIMIT / 2);
		const maxStart = Math.max(0, hunk.lines.length - PATCH_WINDOW_LINE_LIMIT);
		const start = Math.max(
			0,
			Math.min(maxStart, selectedLineIndex - sourceOffset - halfWindow),
		);
		const end = Math.min(hunk.lines.length, start + PATCH_WINDOW_LINE_LIMIT);
		const lines = hunk.lines.slice(start, end);
		return {
			...hunk,
			lines,
			lineIndexOffset: sourceOffset + start,
			lineWindow: { start, end, total: hunk.lines.length },
			changeCount: lines.filter((line) => line.kind !== "context").length,
			patchLineCount: lines.length,
		};
	}

	function getFocusedPatchWindow(file: ReviewFile) {
		const hunkCount = file.hunks.length;
		if (hunkCount === 0) {
			return { hunks: [] as ReviewHunk[], startIndex: 0, endIndex: 0 };
		}
		const section = selectedSkippedSection();
		const focusIndex = Math.max(
			0,
			Math.min(hunkCount - 1, section?.beforeHunkIndex ?? selectedHunkIndex()),
		);
		let startIndex = focusIndex;
		let endIndex = focusIndex + 1;
		const focusedHunk = getPatchWindowHunk(file.hunks[focusIndex], true);
		let lineCount = getRenderableLineCount(focusedHunk);
		let preferBefore = true;

		while (
			endIndex - startIndex < PATCH_WINDOW_HUNK_LIMIT &&
			lineCount < PATCH_WINDOW_LINE_LIMIT &&
			(startIndex > 0 || endIndex < hunkCount)
		) {
			const candidates = preferBefore
				? (["before", "after"] as const)
				: (["after", "before"] as const);
			let added = false;
			for (const candidate of candidates) {
				const nextIndex = candidate === "before" ? startIndex - 1 : endIndex;
				const hunk = file.hunks[nextIndex];
				if (!hunk) continue;
				if (lineCount + hunk.patchLineCount > PATCH_WINDOW_LINE_LIMIT) continue;
				if (candidate === "before") startIndex = nextIndex;
				else endIndex = nextIndex + 1;
				lineCount += hunk.patchLineCount;
				preferBefore = !preferBefore;
				added = true;
				break;
			}
			if (!added) break;
		}

		return {
			hunks: file.hunks
				.slice(startIndex, endIndex)
				.map((hunk) => getPatchWindowHunk(hunk, hunk.id === focusedHunk.id)),
			startIndex,
			endIndex,
		};
	}

	function renderPatchWindowNotice(message: string) {
		return (
			<box paddingX={1} paddingY={0} backgroundColor={theme.bgSurface}>
				<text fg={theme.textMuted} bg={theme.bgSurface}>
					{message}
				</text>
			</box>
		);
	}

	function renderHunkWindowNotice(hunk: ReviewHunk) {
		if (!hunk.lineWindow) return null;
		return renderPatchWindowNotice(
			`Large change group · showing rows ${hunk.lineWindow.start + 1}-${hunk.lineWindow.end} of ${hunk.lineWindow.total}`,
		);
	}

	function renderFocusedSkippedSection(
		file: ReviewFile,
		section: ReviewSkippedSection,
	) {
		const expanded = () => expandedSectionIds().has(section.id);
		const selected = () => selectedSkippedSection()?.id === section.id;
		return (
			<>
				{renderSkippedSectionRow(section, {
					interactive: true,
					selected,
					expanded,
				})}
				<Show when={expanded()}>
					{renderRawDiffBlock(section.rawPatch, file.filetype)}
				</Show>
			</>
		);
	}

	function renderFocusedPatchContent(file: ReviewFile) {
		const window = () => getFocusedPatchWindow(file);
		const trailingSection = () =>
			window().endIndex === file.hunks.length
				? getSkippedSection(file, file.hunks.length)
				: undefined;
		return (
			<box flexDirection="column" gap={0}>
				<box paddingX={1} paddingY={0} backgroundColor={theme.bgSurface}>
					<text fg={theme.textMuted} bg={theme.bgSurface}>
						Large file · showing nearby change groups for responsiveness
					</text>
				</box>
				<Show when={window().startIndex > 0}>
					{renderPatchWindowNotice(
						`${window().startIndex} earlier change group${window().startIndex === 1 ? "" : "s"} hidden`,
					)}
				</Show>
				<For each={window().hunks}>
					{(hunk) => {
						const hunkIndex = () =>
							file.hunks.findIndex((candidate) => candidate.id === hunk.id);
						const section = () => getSkippedSection(file, hunkIndex());
						return (
							<>
								{renderHunkWindowNotice(hunk)}
								<Show when={section()}>
									{(value) => renderFocusedSkippedSection(file, value())}
								</Show>
								{renderHunkBlock(file, hunk, true)}
							</>
						);
					}}
				</For>
				<Show when={trailingSection()}>
					{(section) => renderFocusedSkippedSection(file, section())}
				</Show>
				<Show when={window().endIndex < file.hunks.length}>
					{renderPatchWindowNotice(
						`${file.hunks.length - window().endIndex} later change group${file.hunks.length - window().endIndex === 1 ? "" : "s"} hidden`,
					)}
				</Show>
			</box>
		);
	}

	function renderFileDiffContent(file: ReviewFile, interactive: boolean) {
		if (interactive && shouldUseFocusedPatchRendering(file)) {
			return renderFocusedPatchContent(file);
		}
		if (file.hunks.length === 0) {
			return renderRawDiffBlock(file.rawPatch, file.filetype);
		}
		return (
			<box flexDirection="column" gap={0}>
				<For each={file.hunks}>
					{(hunk, hunkIndex) => (
						<>
							<Show when={getSkippedSection(file, hunkIndex())}>
								{(section) => {
									const expanded = () => expandedSectionIds().has(section().id);
									const selected = () =>
										interactive &&
										selectedSkippedSection()?.id === section().id;
									return (
										<>
											{renderSkippedSectionRow(section(), {
												interactive,
												selected,
												expanded,
											})}
											<Show when={expanded()}>
												{renderRawDiffBlock(section().rawPatch, file.filetype)}
											</Show>
										</>
									);
								}}
							</Show>
							{renderHunkBlock(file, hunk, interactive)}
						</>
					)}
				</For>
				<Show when={getSkippedSection(file, file.hunks.length)}>
					{(section) => {
						const expanded = () => expandedSectionIds().has(section().id);
						const selected = () =>
							interactive && selectedSkippedSection()?.id === section().id;
						return (
							<>
								{renderSkippedSectionRow(section(), {
									interactive,
									selected,
									expanded,
								})}
								<Show when={expanded()}>
									{renderRawDiffBlock(section().rawPatch, file.filetype)}
								</Show>
							</>
						);
					}}
				</Show>
			</box>
		);
	}

	function openFileNoteEditor(file: ReviewFile) {
		setEditingFileNoteValue(selectedFileNote(file));
		setEditingFileNoteKey(file.noteKey);
		setEditorOpen(true);
	}

	function openFileNoteEditorForPath(filePath: string) {
		const key = `unchanged:${filePath}`;
		setEditingFileNoteValue(fileNotes().get(key)?.trim() ?? "");
		setEditingFileNoteKey(key);
		setEditorOpen(true);
	}

	function closeFileNoteEditor() {
		setEditingFileNoteKey(null);
		setEditingFileNoteValue("");
		setEditorOpen(false);
	}

	function saveFileNoteEditor() {
		const key = editingFileNoteKey();
		if (!key) return;
		setFileNotes((prev) => setMapValue(prev, key, editingFileNoteValue()));
		closeFileNoteEditor();
	}

	async function openRangeNoteEditor(
		_file: ReviewFile,
		range: ReviewRangeDraft,
	) {
		const key = buildRangeNoteKey(range);
		setEditingRangeValue(rangeNotes().get(key) ?? "");
		setEditingRange(range);
		setEditorOpen(true);
	}

	function closeRangeNoteEditor() {
		setEditingRange(null);
		setEditingRangeValue("");
		setEditorOpen(false);
		setRangeAnchor(null);
	}

	function saveRangeNoteEditor() {
		const range = editingRange();
		if (!range) return;
		setRangeNotes((prev) =>
			setMapValue(prev, buildRangeNoteKey(range), editingRangeValue()),
		);
		closeRangeNoteEditor();
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
				subtitle: "Add a file or line note before submitting review.",
				variant: "warning",
			});
			return;
		}
		props.attachments.attach(
			new CodeReviewAttachment("code-review", submission),
		);
		props.onClose();
	}

	function beginRangeSelection() {
		const hunk = selectedHunk();
		const line = selectedLine();
		if (!hunk || !line) return;
		const sameSideIndex = getCommentableLines(
			hunk,
			line.side,
			diffView(),
		).findIndex((candidate) => candidate.index === line.index);
		if (sameSideIndex >= 0) {
			setSelectedLineIndex(hunk.id, sameSideIndex);
		}
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

	function focusDiffLine(
		file: ReviewFile,
		hunk: ReviewHunk,
		line: CommentableLine,
		side?: ReviewSide,
	) {
		const hunkIndex = file.hunks.findIndex(
			(candidate) => candidate.id === hunk.id,
		);
		if (hunkIndex >= 0) setActiveHunkIndex(file.id, hunkIndex);
		setSelectedSectionId(file.id, null);
		setMode("patch");
		const lines = getCommentableLines(hunk, side, diffView());
		const lineIndex = lines.findIndex(
			(candidate) => candidate.index === line.index,
		);
		if (lineIndex >= 0) setSelectedLineIndex(hunk.id, lineIndex);
	}

	function handleDiffLineMouseDown(
		file: ReviewFile,
		hunk: ReviewHunk,
		line: CommentableLine,
		event: TuiMouseEvent,
	) {
		if (editorOpen() || event.button !== 0) return;
		event.preventDefault();
		event.stopPropagation();

		setRangeAnchor(null);
		focusDiffLine(file, hunk, line);
		void openRangeNoteEditor(file, {
			path: file.path,
			side: line.side,
			startLine: line.lineNumber,
			endLine: line.lineNumber,
		});
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: editorOpen,
		diagnosticsWhen: editorOpen,
		commands: {
			"review.close-editor": () => {
				if (viewingFileEditingRange()) {
					setViewingFileEditingRange(null);
					setViewingFileEditingValue("");
					setEditorOpen(false);
				} else if (editingRange()) {
					closeRangeNoteEditor();
				} else if (editingFileNoteKey()) {
					closeFileNoteEditor();
				}
			},
		},
	}));

	// Patch mode — diff view (changed files)
	useKeymapLayer(() => ({
		scope: "modal",
		when: () => !editorOpen() && mode() === "patch" && !viewingFilePath(),
		diagnosticsWhen: () => mode() === "patch",
		commands: {
			"review.back": () => {
				if (rangeAnchor()) setRangeAnchor(null);
				else setMode("tree");
			},
			"review.previous-change": () => cycleHunk(-1),
			"review.next-change": () => cycleHunk(1),
			"review.move-line-up": () => moveSelectedLine(-1),
			"review.move-line-down": () => moveSelectedLine(1),
			"review.toggle-section": () => {
				const section = selectedSkippedSection();
				if (section) toggleExpandedContext(section.id);
			},
			"review.comment-line": () => confirmSelectedLineComment(),
			"review.start-range": () => beginRangeSelection(),
			"review.file-note": () => {
				const file = selectedFile();
				if (file) openFileNoteEditor(file);
			},
			"review.toggle-view": () => toggleDiffView(),
			"review.clear-line-note": () => clearOrCancelLineSelection(),
			"review.submit": () => submitReview(),
		},
	}));

	// Patch mode — read-only view (unchanged files)
	useKeymapLayer(() => ({
		scope: "modal",
		when: () => !editorOpen() && mode() === "patch" && !!viewingFilePath(),
		diagnosticsWhen: () => mode() === "patch",
		commands: {
			"review.back": () => {
				setViewingFilePath(null);
				setViewingFileEditingRange(null);
				setViewingFileEditingValue("");
				setMode("tree");
			},
			"review.move-line-up": () => {
				setViewingFileLine((l) => Math.max(1, l - 1));
			},
			"review.move-line-down": () => {
				setViewingFileLine((l) => Math.min(viewingFileLineCount(), l + 1));
			},
			"review.comment-line": () => {
				const vp = viewingFilePath();
				if (!vp) return;
				const line = viewingFileLine();
				const range: ReviewRangeDraft = {
					path: vp,
					side: "additions",
					startLine: line,
					endLine: line,
				};
				setViewingFileEditingValue(
					rangeNotes().get(buildRangeNoteKey(range))?.trim() ?? "",
				);
				setViewingFileEditingRange(range);
				setEditorOpen(true);
			},
			"review.file-note": () => {
				const vp = viewingFilePath();
				if (vp) openFileNoteEditorForPath(vp);
			},
			"review.clear-line-note": () => {
				const vp = viewingFilePath();
				if (!vp) return;
				const key = buildRangeNoteKey({
					path: vp,
					side: "additions",
					startLine: viewingFileLine(),
					endLine: viewingFileLine(),
				});
				setRangeNotes((prev) => setMapValue(prev, key, ""));
			},
			"review.submit": () => submitReview(),
		},
	}));

	// Tree mode: view-level bindings (navigation is handled by FileTreePanel)
	useKeymapLayer(() => ({
		scope: "modal",
		when: () => !editorOpen() && mode() === "tree" && !fileFinderOpen(),
		diagnosticsWhen: () => mode() === "tree" && !fileFinderOpen(),
		commands: {
			"review.file-note": () => {
				const file = selectedFile();
				if (file) openFileNoteEditor(file);
			},
			"review.toggle-view": () => toggleDiffView(),
			"review.clear-file-note": () => clearSelectedFileNote(),
			"review.submit": () => submitReview(),
		},
	}));

	return (
		<ScreenLayout
			surfaceProps={props.surfaceProps}
			zIndex={1200}
			header={
				<ScreenHeader
					left={<text fg={theme.textMuted}>Code review</text>}
					right={
						<text fg={theme.textMuted}>
							{formatFileCount(reviewFiles().length)}
							{totalDraftNotes() > 0
								? ` · ${formatNoteCount(totalDraftNotes())}`
								: ""}
						</text>
					}
				/>
			}
			footer={
				<KeymapHintBar
					group="review"
					prefixBindings={
						mode() === "patch" && !editorOpen()
							? [{ key: "Click", action: "comment" }]
							: undefined
					}
				/>
			}
		>
			<Show
				when={!files.loading}
				fallback={
					<box flexGrow={1} padding={1}>
						<text fg={theme.textMuted}>Loading code review…</text>
					</box>
				}
			>
				<box
					ref={(el) => {
						contentRef = el as typeof contentRef;
					}}
					onSizeChange={() => {
						const w = contentRef?.width ?? 0;
						if (w > 0) setContentWidth(w);
					}}
					flexGrow={1}
					flexDirection="row"
					overflow="hidden"
				>
					{/* Tree panel */}
					<Show when={isWide() || mode() === "tree"}>
						<box
							width={isWide() ? treePanelWidth() : undefined}
							flexGrow={isWide() ? 0 : 1}
							flexShrink={0}
							height="100%"
							border={isWide() ? ["right"] : false}
							borderColor={theme.borderDefault}
						>
							<FileTreePanel
								reviewFiles={reviewFiles()}
								allFiles={allFiles() ?? []}
								focused={mode() === "tree"}
								editorOpen={editorOpen()}
								finderOpen={fileFinderOpen()}
								onFocusedPathChange={(path) => {
									setTreeFocusedPath(path);
									// Sync selectedIndex for diff state
									if (path) {
										const idx = reviewFiles().findIndex((f) => f.path === path);
										if (idx >= 0) setSelectedIndex(idx);
									}
								}}
								onSelectFile={selectFilePath}
								onOpenFileFinder={() => setFileFinderOpen(true)}
								onClose={props.onClose}
							/>
						</box>
					</Show>

					{/* Diff pane */}
					<Show when={isWide() || mode() === "patch"}>
						<Show
							when={selectedFile()}
							fallback={
								<box
									flexGrow={1}
									height="100%"
									flexDirection="column"
									overflow="hidden"
								>
									<Show
										when={viewingFilePath()}
										fallback={
											<box
												flexGrow={1}
												justifyContent="center"
												alignItems="center"
											>
												<text fg={theme.textMuted}>Select a file to view</text>
											</box>
										}
									>
										{(filePath) => (
											<ReadOnlyFileView
												repoRoot={repoRoot}
												path={filePath()}
												paneWidth={diffPaneWidth()}
												interactive={mode() === "patch"}
												selectedLine={viewingFileLine()}
												onLineCountChange={setViewingFileLineCount}
												fileNote={
													fileNotes().get(`unchanged:${filePath()}`)?.trim() ??
													""
												}
												editingFileNote={
													editingFileNoteKey() === `unchanged:${filePath()}`
												}
												editingFileNoteValue={editingFileNoteValue()}
												onEditingFileNoteChange={setEditingFileNoteValue}
												onEditingFileNoteSubmit={saveFileNoteEditor}
												rangeNotes={rangeNotes()}
												editingRange={viewingFileEditingRange()}
												editingRangeValue={viewingFileEditingValue()}
												onEditingRangeChange={setViewingFileEditingValue}
												onEditingRangeSubmit={() => {
													const range = viewingFileEditingRange();
													if (!range) return;
													const key = buildRangeNoteKey(range);
													setRangeNotes((prev) =>
														setMapValue(prev, key, viewingFileEditingValue()),
													);
													setViewingFileEditingRange(null);
													setViewingFileEditingValue("");
													setEditorOpen(false);
												}}
											/>
										)}
									</Show>
								</box>
							}
						>
							{(file) => {
								const currentHunk = createMemo(() => selectedHunk());
								const fileNote = createMemo(() => selectedFileNote(file()));
								return (
									<box
										flexGrow={1}
										height="100%"
										flexDirection="column"
										gap={1}
										backgroundColor={theme.bgMuted}
										overflow="hidden"
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
													{reviewStatusLabel(file())} {file().path}
												</text>
												<Show when={file().prevPath}>
													<text fg={theme.textMuted}>
														from {file().prevPath}
													</text>
												</Show>
												<Show when={activeLineStatus().length > 0}>
													<text fg={theme.textMuted}>{activeLineStatus()}</text>
												</Show>
												<Show when={hiddenContextStatus().length > 0}>
													<text fg={theme.metaText}>
														{hiddenContextStatus()}
													</text>
												</Show>
											</box>
											<text fg={theme.textMuted}>
												{sourceLabel(file()) ? `${sourceLabel(file())} · ` : ""}
												{currentHunk()
													? `change group ${selectedHunkIndex() + 1}/${file().hunks.length}`
													: `${file().hunks.length} change group${file().hunks.length === 1 ? "" : "s"}`}
											</text>
										</box>

										<Show
											when={
												editingFileNoteKey() === file().noteKey ||
												fileNote().length > 0
											}
										>
											<box
												flexShrink={0}
												marginX={1}
												flexDirection="column"
												gap={0}
											>
												{renderFileNoteBlock(file())}
											</box>
										</Show>

										<box
											flexGrow={1}
											padding={1}
											paddingTop={0}
											backgroundColor={theme.bg}
											overflow="hidden"
										>
											<scrollbox
												ref={(value) => {
													if (!value) return;
													patchScrollRef = value as PatchScrollRef;
													applyPendingPatchScrollReset(patchScrollRef);
												}}
												flexGrow={1}
												scrollY
											>
												{renderFileDiffContent(file(), mode() === "patch")}
											</scrollbox>
										</box>
									</box>
								);
							}}
						</Show>
					</Show>
				</box>
				<Show when={fileFinderOpen()}>
					<FileFinderDialog
						options={fileFinderOptions()}
						loading={allFiles.loading}
						onClose={() => setFileFinderOpen(false)}
					/>
				</Show>
			</Show>
		</ScreenLayout>
	);
}

// ── File finder dialog ──────────────────────────────────────────────

type FileFinderDialogProps = {
	options: PickerOption[];
	loading: boolean;
	onClose: () => void;
};

function FileFinderDialog(props: FileFinderDialogProps) {
	const picker = createPickerManager();
	let didShow = false;

	createEffect(() => {
		if (didShow) return;
		didShow = true;
		picker.show({
			label: "Find file",
			options: props.options,
			loading: props.loading,
			filterable: true,
			onDismiss: props.onClose,
		});
	});

	createEffect(() => {
		picker.updateOptions(props.options);
		picker.setLoading(props.loading);
	});

	return (
		<Show when={picker.current().visible}>
			<Dialog.Root
				width="80%"
				minWidth={72}
				maxWidth={120}
				height={18}
				padding={0}
			>
				<box flexGrow={1} flexDirection="column">
					<Picker.Root
						picker={picker}
						maxVisible={12}
						commandNamespace="review-file-finder"
					>
						<Picker.Header />
						<Picker.Body />
						<Picker.Footer flexDirection="column">
							<Show when={props.loading}>
								<text fg={theme.textMuted}>Loading repository files…</text>
							</Show>
							<KeymapHintBar borderless group="review-file-finder" />
						</Picker.Footer>
					</Picker.Root>
				</box>
			</Dialog.Root>
		</Show>
	);
}

// ── Read-only file viewer ───────────────────────────────────────────

type ReadOnlyFileViewProps = {
	repoRoot: string;
	path: string;
	interactive: boolean;
	selectedLine: number;
	onLineCountChange: (count: number) => void;
	fileNote: string;
	editingFileNote: boolean;
	editingFileNoteValue: string;
	onEditingFileNoteChange: (value: string) => void;
	onEditingFileNoteSubmit: () => void;
	rangeNotes: Map<string, string>;
	editingRange: ReviewRangeDraft | null;
	editingRangeValue: string;
	onEditingRangeChange: (value: string) => void;
	onEditingRangeSubmit: () => void;
	/** Available pane width for computing wrap-aware line heights. */
	paneWidth: number;
};

function ReadOnlyFileView(props: ReadOnlyFileViewProps) {
	const content = createMemo(() => {
		try {
			return readFileSync(path.join(props.repoRoot, props.path), "utf-8");
		} catch {
			return null;
		}
	});

	const filetype = createMemo(() => inferFiletype(props.path));
	const lines = createMemo(() => {
		const text = content();
		if (!text) return [];
		return text.replace(/\r\n/g, "\n").split("\n");
	});
	const lineNumberWidth = createMemo(() =>
		Math.max(1, String(lines().length).length),
	);
	// Content area = paneWidth minus padding={1} (both sides) and the gutter
	// (line number column + the two-space separator).
	const contentColumns = createMemo(() =>
		Math.max(10, props.paneWidth - 2 - lineNumberWidth() - 2),
	);

	createEffect(() => {
		props.onLineCountChange(lines().length);
	});

	type ReadOnlyScrollRef = {
		scrollTop?: number;
		viewport?: { height: number };
		scrollChildIntoView?: (id: string) => void;
		scrollTo?: (position: number | { x: number; y: number }) => void;
	};
	let scrollRef: ReadOnlyScrollRef | undefined;
	let scrollTimeout: ReturnType<typeof setTimeout> | undefined;
	let resetScrollTimeout: ReturnType<typeof setTimeout> | undefined;

	function clearReadonlyScrollTimeout() {
		if (!scrollTimeout) return;
		clearTimeout(scrollTimeout);
		scrollTimeout = undefined;
	}

	function clearReadonlyResetScrollTimeout() {
		if (!resetScrollTimeout) return;
		clearTimeout(resetScrollTimeout);
		resetScrollTimeout = undefined;
	}

	function lineRowOffset(lineIndex: number): number {
		const all = lines();
		const cols = contentColumns();
		let top = 0;
		const end = Math.min(lineIndex, all.length);
		for (let i = 0; i < end; i += 1) {
			top += Math.max(1, estimateWrappedRows(all[i], cols));
		}
		return top;
	}

	function resetScrollToTop() {
		const ref = scrollRef;
		if (!ref) return;
		if (ref.scrollTo) {
			ref.scrollTo(0);
		} else if (typeof ref.scrollTop === "number") {
			ref.scrollTop = 0;
		} else {
			ref.scrollChildIntoView?.(`readonly-line-1`);
		}
	}

	// Reset the scroll position whenever the file path/content changes. Runs
	// as its own effect so it's independent of the cursor visibility logic and
	// fires for every path transition (including the initial mount).
	createEffect(() => {
		void props.path;
		void lines();
		clearReadonlyResetScrollTimeout();
		resetScrollTimeout = setTimeout(() => {
			resetScrollTimeout = undefined;
			resetScrollToTop();
		}, 0);
		onCleanup(clearReadonlyResetScrollTimeout);
	});

	createEffect(() => {
		if (!props.interactive) return;
		const line = props.selectedLine;
		// Track paneWidth and lines so we re-scroll after the layout reflows
		// when the terminal resizes, the diff/tree split changes, or the file
		// content swaps in.
		void props.paneWidth;
		void lines();
		void contentColumns();
		clearReadonlyScrollTimeout();
		scrollTimeout = setTimeout(() => {
			scrollTimeout = undefined;
			const ref = scrollRef;
			if (!ref) return;
			const cursorTop = lineRowOffset(line - 1);
			const cursorHeight = Math.max(
				1,
				estimateWrappedRows(lines()[line - 1] ?? "", contentColumns()),
			);
			const current = ref.scrollTop ?? 0;
			const vh = ref.viewport?.height ?? 0;
			// Keep the cursor in view: scroll up when it's above the viewport,
			// scroll down when it's below. Otherwise leave the scroll alone.
			if (cursorTop < current) {
				if (ref.scrollTo) ref.scrollTo(cursorTop);
				else if (typeof ref.scrollTop === "number") ref.scrollTop = cursorTop;
				else ref.scrollChildIntoView?.(`readonly-line-${line}`);
			} else if (vh > 0 && cursorTop + cursorHeight > current + vh) {
				const target = Math.max(0, cursorTop + cursorHeight - vh);
				if (ref.scrollTo) ref.scrollTo(target);
				else if (typeof ref.scrollTop === "number") ref.scrollTop = target;
				else ref.scrollChildIntoView?.(`readonly-line-${line}`);
			} else if (vh === 0) {
				// Initial mount before viewport is sized — fall back to the
				// scrollbox's own measurement so the first scroll still lands.
				ref.scrollChildIntoView?.(`readonly-line-${line}`);
			}
		}, 0);
		onCleanup(clearReadonlyScrollTimeout);
	});

	const annotationsByLine = createMemo(() => {
		const map = new Map<number, { key: string; comment: string }>();
		const prefix = `${props.path}::`;
		for (const [key, value] of props.rangeNotes) {
			if (!key.startsWith(prefix)) continue;
			const comment = value.trim();
			if (!comment) continue;
			const parsed = parseRangeNoteKey(key);
			if (!parsed) continue;
			for (let l = parsed.startLine; l <= parsed.endLine; l++) {
				map.set(l, { key, comment });
			}
		}
		const editing = props.editingRange;
		if (editing && editing.path === props.path) {
			for (let l = editing.startLine; l <= editing.endLine; l++) {
				map.set(l, { key: "editing", comment: props.editingRangeValue });
			}
		}
		return map;
	});

	type ScrollableRef = { scrollX: number; scrollY: number };
	function resetScroll(ref: ScrollableRef | undefined) {
		queueMicrotask(() => {
			if (!ref) return;
			ref.scrollX = 0;
			ref.scrollY = 0;
		});
	}

	let composerRef: { plainText: string } | undefined;
	let fileNoteComposerRef: { plainText: string } | undefined;

	return (
		<box flexGrow={1} flexDirection="column" backgroundColor={theme.bgMuted}>
			<box
				flexShrink={0}
				paddingX={1}
				paddingY={0}
				flexDirection="row"
				justifyContent="space-between"
			>
				<text fg={theme.textPrimary}>{props.path}</text>
				<Show when={lines().length > 0}>
					<text fg={theme.textMuted}>
						{lines().length} line{lines().length === 1 ? "" : "s"}
					</text>
				</Show>
			</box>

			<Show when={props.editingFileNote || props.fileNote}>
				<Show
					when={props.editingFileNote}
					fallback={
						<box
							flexShrink={0}
							marginX={1}
							border
							borderColor={theme.borderDefault}
							backgroundColor={theme.bgSurface}
							paddingX={1}
						>
							<text fg={theme.textPrimary}>{props.fileNote}</text>
						</box>
					}
				>
					<box flexShrink={0} marginX={1}>
						<MessageComposer
							initialValue={props.editingFileNoteValue}
							placeholder="Comment on the whole file..."
							backgroundColor={theme.bgTransparent}
							focusedBackgroundColor={theme.bgTransparent}
							keyBindings={[
								{ name: "return", action: "submit" },
								{ name: "return", shift: true, action: "newline" },
							]}
							onContentChange={() =>
								props.onEditingFileNoteChange(
									fileNoteComposerRef?.plainText ?? "",
								)
							}
							onSubmit={props.onEditingFileNoteSubmit}
							ref={(el) => {
								fileNoteComposerRef = el as typeof fileNoteComposerRef;
							}}
						/>
					</box>
				</Show>
			</Show>

			<box flexGrow={1} padding={1} paddingTop={0} backgroundColor={theme.bg}>
				<Show
					when={content() != null}
					fallback={<text fg={theme.textMuted}>Could not read file</text>}
				>
					<scrollbox
						ref={(el) => {
							scrollRef = el as typeof scrollRef;
						}}
						flexGrow={1}
						scrollY
					>
						<box flexDirection="column" gap={0}>
							<For each={lines()}>
								{(line, idx) => {
									const lineNum = () => idx() + 1;
									const active = () =>
										props.interactive && lineNum() === props.selectedLine;
									const bg = () => (active() ? theme.diffCursorBg : theme.bg);
									const gutterBg = () =>
										active() ? theme.diffCursorGutterBg : theme.bg;
									const annotation = () =>
										annotationsByLine().get(lineNum()) ?? null;
									const rowHeight = () =>
										Math.max(1, estimateWrappedRows(line, contentColumns()));
									let lineRef: ScrollableRef | undefined;
									return (
										<>
											<box
												id={`readonly-line-${lineNum()}`}
												flexDirection="row"
												alignItems="flex-start"
												height={rowHeight()}
												flexShrink={0}
												backgroundColor={bg()}
											>
												<text
													fg={theme.textMuted}
													bg={gutterBg()}
													flexShrink={0}
													height={1}
												>
													{String(lineNum()).padStart(lineNumberWidth())}
												</text>
												<text
													fg={theme.textMuted}
													bg={bg()}
													flexShrink={0}
													height={1}
												>
													{"  "}
												</text>
												<Show
													when={filetype()}
													fallback={
														<text
															ref={(el) => {
																lineRef = el as ScrollableRef | undefined;
															}}
															fg={theme.textPrimary}
															bg={bg()}
															flexGrow={1}
															wrapMode="word"
															height={rowHeight()}
															onMouseScroll={() => resetScroll(lineRef)}
														>
															{line}
														</text>
													}
												>
													{(ft) => (
														<code
															ref={(el) => {
																lineRef = el as ScrollableRef | undefined;
															}}
															content={line}
															filetype={ft()}
															syntaxStyle={syntaxStyle()}
															bg={bg()}
															conceal={false}
															wrapMode="word"
															flexGrow={1}
															height={rowHeight()}
															onMouseScroll={() => resetScroll(lineRef)}
														/>
													)}
												</Show>
											</box>
											<Show when={annotation()}>
												{(ann) => (
													<Show
														when={ann().key === "editing"}
														fallback={
															<box
																border
																borderColor={theme.borderDefault}
																backgroundColor={theme.bgSurface}
																paddingX={1}
																width="100%"
																flexShrink={0}
															>
																<text fg={theme.textPrimary}>
																	{ann().comment}
																</text>
															</box>
														}
													>
														<MessageComposer
															ref={(el) => {
																composerRef = el as typeof composerRef;
															}}
															initialValue={props.editingRangeValue}
															placeholder="Type your review note..."
															backgroundColor={theme.bgTransparent}
															focusedBackgroundColor={theme.bgTransparent}
															keyBindings={[
																{
																	name: "return",
																	action: "submit",
																},
																{
																	name: "return",
																	shift: true,
																	action: "newline",
																},
															]}
															onContentChange={() =>
																props.onEditingRangeChange(
																	composerRef?.plainText ?? "",
																)
															}
															onSubmit={props.onEditingRangeSubmit}
														/>
													</Show>
												)}
											</Show>
										</>
									);
								}}
							</For>
						</box>
					</scrollbox>
				</Show>
			</box>
		</box>
	);
}
