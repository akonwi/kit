import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import { type DiffLineAnnotation, trimPatchContext } from "@pierre/diffs";
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
import {
	DASHED_VERTICAL,
	DIAMOND,
	PENCIL,
	TRIANGLE_DOWN,
	TRIANGLE_RIGHT,
} from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { MessageComposer, type TextareaRef } from "../../shell/MessageComposer";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { theme } from "../../shell/theme";
import type { ToastInput } from "../../state/toasts";
import { CodeReviewAttachment } from "./attachment";
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
	type ReviewSkippedSection,
} from "./model";
import {
	getReviewDiffActiveLineId,
	getReviewDiffCommentableLines,
	getReviewDiffLineTop,
	getReviewDiffRangeBounds,
	type ReviewDiffAnnotationMetadata,
	ReviewDiffBlock,
	type ReviewDiffCommentableLine,
} from "./ReviewDiffBlock";

export type ReviewContentProps = {
	onClose: () => void;
	attachments: AttachmentsController;
	toast: (toast: ToastInput) => void;
	defaultDiffView: ReviewDiffView;
	surfaceProps?: OverlayComponentProps<void>["surfaceProps"];
};

type ReviewMode = "list" | "patch";
type ReviewSide = "additions" | "deletions";
type CommentableLine = ReviewDiffCommentableLine;

const LIST_AUTO_EXPAND_FILE_LIMIT = 12;
const LIST_DIFF_PREVIEW_HUNK_LIMIT = 3;
const LIST_DIFF_PREVIEW_LINE_LIMIT = 120;
const PATCH_FOCUSED_RENDER_HUNK_LIMIT = 40;
const PATCH_FOCUSED_RENDER_LINE_LIMIT = 800;
const PATCH_WINDOW_HUNK_LIMIT = 12;
const PATCH_WINDOW_LINE_LIMIT = 800;

type RangeAnchor = {
	side: ReviewSide;
	lineNumber: number;
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

function formatFileCount(count: number): string {
	return `${count} file${count === 1 ? "" : "s"}`;
}

function getReviewFileRenderedLineCount(file: ReviewFile): number {
	if (file.hunks.length === 0) {
		return file.rawPatch.replace(/\r\n/g, "\n").split("\n").length;
	}
	return file.unifiedLineCount;
}

function shouldAutoExpandReviewList(files: ReviewFile[]): boolean {
	return files.length <= LIST_AUTO_EXPAND_FILE_LIMIT;
}

function shouldUseFocusedPatchRendering(file: ReviewFile): boolean {
	return (
		file.hunks.length > PATCH_FOCUSED_RENDER_HUNK_LIMIT ||
		getReviewFileRenderedLineCount(file) > PATCH_FOCUSED_RENDER_LINE_LIMIT
	);
}

function sourceLabel(file: ReviewFile): string {
	switch (file.source) {
		case "staged":
			return "staged";
		case "unstaged":
			return "unstaged";
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
): number {
	return getReviewDiffLineTop(hunk, lineIndex, diffView, annotations);
}

function getVisualBoundsForRange(
	hunk: ReviewHunk,
	range: ReviewRangeDraft,
	diffView: ReviewDiffView,
	annotations: DiffLineAnnotation<ReviewDiffAnnotationMetadata>[] = [],
) {
	return getReviewDiffRangeBounds(hunk, range, diffView, annotations);
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
	const patchScrollRefs = new Map<string, PatchScrollRef>();
	let listScrollRef: ScrollRef | undefined;
	let listCursorScrollTimeout: ReturnType<typeof setTimeout> | undefined;
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
			setExpandedKeys(new Set<string>());
			setSelectedSectionIds(new Map<string, string>());
			setExpandedSectionIds(new Set<string>());
			setMode("list");
			setRangeAnchor(null);
			return;
		}
		setExpandedKeys((prev) => {
			if (prev.size > 0) return prev;
			if (!shouldAutoExpandReviewList(list)) return prev;
			return new Set<string>(list.map((file) => file.id));
		});
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
		clearListCursorScrollTimeout();
		if (mode() !== "list") return;
		const file = selectedFile();
		expandedKeys();
		if (!file) return;
		listCursorScrollTimeout = setTimeout(() => {
			listCursorScrollTimeout = undefined;
			listScrollRef?.scrollChildIntoView(`review-file-row-${file.id}`);
		}, 0);
		onCleanup(clearListCursorScrollTimeout);
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
		if (!childId) return;

		patchCursorScrollTimeout = setTimeout(() => {
			patchCursorScrollTimeout = undefined;
			patchScrollRefs.get(file.id)?.scrollChildIntoView?.(childId);
		}, 0);
		onCleanup(clearPatchCursorScrollTimeout);
	});

	function clearListCursorScrollTimeout() {
		if (!listCursorScrollTimeout) return;
		clearTimeout(listCursorScrollTimeout);
		listCursorScrollTimeout = undefined;
	}

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
		if (!hunk || !line) return;
		const nextLines = getCommentableLines(hunk, rangeAnchor()?.side, nextView);
		const nextIndex = nextLines.findIndex(
			(candidate) => candidate.index === line.index,
		);
		if (nextIndex >= 0) {
			setSelectedLineIndex(hunk.id, nextIndex);
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

	function getListPreviewHunks(file: ReviewFile): ReviewHunk[] {
		const hunks: ReviewHunk[] = [];
		let lineCount = 0;
		for (const hunk of file.hunks) {
			if (hunks.length >= LIST_DIFF_PREVIEW_HUNK_LIMIT) break;
			const remainingLines = LIST_DIFF_PREVIEW_LINE_LIMIT - lineCount;
			if (remainingLines <= 0) break;
			const lines = hunk.lines.slice(0, remainingLines);
			if (lines.length === 0) continue;
			hunks.push({
				...hunk,
				id: `${hunk.id}:preview`,
				lines,
				changeCount: lines.filter((line) => line.kind !== "context").length,
				patchLineCount: lines.length,
			});
			lineCount += lines.length;
		}
		return hunks;
	}

	function limitRawPatch(rawPatch: string): string {
		const lines = rawPatch.replace(/\r\n/g, "\n").split("\n");
		if (lines.length <= LIST_DIFF_PREVIEW_LINE_LIMIT) return rawPatch;
		return trimPatchContext(rawPatch, 3);
	}

	function renderPreviewNotice(file: ReviewFile, visibleHunkCount: number) {
		const hiddenHunks = Math.max(0, file.hunks.length - visibleHunkCount);
		const hiddenLineCount = Math.max(
			0,
			getReviewFileRenderedLineCount(file) - LIST_DIFF_PREVIEW_LINE_LIMIT,
		);
		if (hiddenHunks === 0 && hiddenLineCount === 0) return null;
		return (
			<box paddingX={1} paddingY={0} backgroundColor={theme.bgSurface}>
				<text fg={theme.textMuted} bg={theme.bgSurface}>
					Preview only · press Enter for full file
					{hiddenHunks > 0
						? ` · ${hiddenHunks} more change group${hiddenHunks === 1 ? "" : "s"}`
						: ""}
				</text>
			</box>
		);
	}

	function renderListFileDiffContent(file: ReviewFile) {
		if (!shouldUseFocusedPatchRendering(file)) {
			return renderFileDiffContent(file, false);
		}
		if (file.hunks.length === 0) {
			return (
				<box flexDirection="column" gap={0}>
					{renderRawDiffBlock(limitRawPatch(file.rawPatch), file.filetype)}
					{renderPreviewNotice(file, 0)}
				</box>
			);
		}
		const hunks = getListPreviewHunks(file);
		return (
			<box flexDirection="column" gap={0}>
				<For each={hunks}>{(hunk) => renderHunkBlock(file, hunk, false)}</For>
				{renderPreviewNotice(file, hunks.length)}
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
		setExpandedKeys((prev) => {
			if (prev.has(file.id)) return prev;
			return new Set([...prev, file.id]);
		});
		setEditingFileNoteValue(selectedFileNote(file));
		setEditingFileNoteKey(file.noteKey);
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
				if (editingRange()) closeRangeNoteEditor();
				else if (editingFileNoteKey()) closeFileNoteEditor();
			},
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => !editorOpen() && mode() === "patch",
		diagnosticsWhen: () => mode() === "patch",
		commands: {
			"review.back": () => {
				if (rangeAnchor()) setRangeAnchor(null);
				else setMode("list");
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

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => !editorOpen() && mode() === "list",
		diagnosticsWhen: () => mode() === "list",
		commands: {
			"review.close": () => props.onClose(),
			"review.move-file-up": () => {
				setSelectedIndex((index) => Math.max(0, index - 1));
			},
			"review.move-file-down": () => {
				setSelectedIndex((index) =>
					Math.min(reviewFiles().length - 1, index + 1),
				);
			},
			"review.focus-file": () => {
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
			},
			"review.toggle-file": () => {
				const file = selectedFile();
				if (file) toggleExpanded(file.id);
			},
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
									<Show when={!shouldAutoExpandReviewList(reviewFiles())}>
										<box
											paddingX={1}
											paddingY={0}
											backgroundColor={theme.bgSurface}
										>
											<text fg={theme.textMuted} bg={theme.bgSurface}>
												Large review · files start collapsed for responsiveness
											</text>
										</box>
									</Show>
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
														id={`review-file-row-${file.id}`}
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
																{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}{" "}
																{statusLabel(file)} {file.path}
																{noteCount() > 0
																	? ` · ${PENCIL} ${formatNoteCount(noteCount())}`
																	: ""}
															</text>
															<Show when={file.prevPath}>
																<text fg={theme.textMuted}>
																	from {file.prevPath}
																</text>
															</Show>
														</box>
														<text fg={theme.textMuted}>
															{sourceLabel(file)} · {file.hunks.length} hunk
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
															{renderFileNoteBlock(file)}
															{renderListFileDiffContent(file)}
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
											<Show when={hiddenContextStatus().length > 0}>
												<text fg={theme.metaText}>{hiddenContextStatus()}</text>
											</Show>
										</box>
										<text fg={theme.textMuted}>
											{sourceLabel(file())} ·{" "}
											{currentHunk()
												? `change group ${selectedHunkIndex() + 1}/${file().hunks.length}`
												: `${file().hunks.length} change group${file().hunks.length === 1 ? "" : "s"}`}
										</text>
									</box>

									<Show
										when={
											editingFileNoteKey() === file().noteKey ||
											fileNote().length > 0 ||
											selectedRangeNote().length > 0
										}
									>
										<box
											flexShrink={0}
											marginX={1}
											flexDirection="column"
											gap={0}
										>
											{renderFileNoteBlock(file())}
											<Show when={selectedRangeNote().length > 0}>
												<box
													border
													borderColor={theme.borderDefault}
													paddingX={1}
													flexDirection="column"
													gap={0}
												>
													<text fg={theme.textPrimary}>
														{currentLineNoteLabel()}: {selectedRangeNote()}
													</text>
												</box>
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
											{renderFileDiffContent(file(), true)}
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
