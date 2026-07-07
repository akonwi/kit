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
	CHEVRON_RIGHT,
	CIRCLE_FILLED,
	DASHED_VERTICAL,
	DIAMOND,
	MIDDLE_DOT,
	TRIANGLE_DOWN,
	TRIANGLE_RIGHT,
} from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { MessageComposer, type TextareaRef } from "../../shell/MessageComposer";
import { Picker } from "../../shell/Picker";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { scrollbarStyle, syntaxStyle, theme } from "../../shell/theme";
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
	getCurrentBranch,
	getMergeBase,
	getRepoRoot,
	isAncestorOfHead,
	listLocalBranches,
	listRecentCommits,
	listRepoFiles,
	loadReviewFiles,
	type ReviewBranchSummary,
	type ReviewCommitSummary,
	type ReviewFile,
	type ReviewHunk,
	type ReviewLine,
	type ReviewSkippedSection,
	type ReviewTarget,
	resolveCommit,
	resolveCommitParent,
	resolveDefaultBranchBase,
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

const WIDE_VIEWPORT_THRESHOLD = 121;
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
	scrollLeft?: number;
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
		case "commit":
			// The screen header crumb already names the commit target;
			// repeating it per file would be noise.
			return "";
	}
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

/**
 * Builds a synthetic context-only hunk for a skipped (unchanged) section
 * so its expanded view renders through the structured hunk path — with a
 * line-number gutter — instead of the numberless raw-patch fallback.
 */
function skippedSectionToHunk(section: ReviewSkippedSection): ReviewHunk {
	const raw = section.rawPatch.replace(/\r\n/g, "\n").split("\n");
	const hunkMarker = raw.findIndex((line) => line.startsWith("@@"));
	const body = hunkMarker >= 0 ? raw.slice(hunkMarker + 1) : raw;
	const lines: ReviewLine[] = body.map((line, i) => ({
		kind: "context",
		text: line.startsWith(" ") ? line.slice(1) : line,
		additionLineNumber:
			section.additionStart > 0 ? section.additionStart + i : undefined,
		deletionLineNumber:
			section.deletionStart > 0 ? section.deletionStart + i : undefined,
	}));
	return {
		id: section.id,
		noteKey: section.id,
		header: "",
		context: "",
		lines,
		changeCount: 0,
		rawPatch: section.rawPatch,
		patchStartLine: 0,
		patchLineCount: lines.length,
		additionStart: section.additionStart,
		additionCount: lines.length,
		deletionStart: section.deletionStart,
		deletionCount: lines.length,
		collapsedBefore: 0,
	};
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

/**
 * File-wide line-number column width, covering every hunk and skipped
 * section, so gutters align vertically across the whole file instead of
 * each block sizing to its own max line number.
 */
function lineNumberWidthForFile(file: ReviewFile): number {
	let width = 1;
	for (const hunk of file.hunks) {
		width = Math.max(width, lineNumberWidthForHunk(hunk));
	}
	for (const section of file.skippedSections) {
		const end =
			Math.max(section.additionStart, section.deletionStart) +
			section.lineCount -
			1;
		width = Math.max(width, String(Math.max(1, end)).length);
	}
	return width;
}

// Hunk content chrome widths (matches the layout in renderHunkBlock):
//   the patch box is full-bleed (no horizontal padding) so rows align
//   flush with the pane header above
//   each hunk wrapper has paddingLeft={2} (comment-marker lane)
const PATCH_CONTENT_PADDING = 0;
const HUNK_PADDING_LEFT = 2;

function unifiedContentColumns(lnw: number, diffPaneWidth: number): number {
	// Unified row: [lnw][space][lnw][sign][space]
	const gutterCols = 2 * lnw + 3;
	return Math.max(
		10,
		diffPaneWidth - PATCH_CONTENT_PADDING - HUNK_PADDING_LEFT - gutterCols,
	);
}

function splitContentColumns(lnw: number, diffPaneWidth: number): number {
	const inner = diffPaneWidth - PATCH_CONTENT_PADDING - HUNK_PADDING_LEFT;
	const halfWidth = Math.floor(inner / 2);
	// Split cell: [lnw][sign][space]
	return Math.max(10, halfWidth - lnw - 2);
}

function contentColumnsFor(
	file: ReviewFile,
	view: ReviewDiffView,
	diffPaneWidth: number,
): number {
	const lnw = lineNumberWidthForFile(file);
	return view === "split"
		? splitContentColumns(lnw, diffPaneWidth)
		: unifiedContentColumns(lnw, diffPaneWidth);
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
	// What the review is diffing: working tree (default), one commit, or a
	// branch's total diff. See docs/features/code-review-commit-targets.md.
	const [target, setTarget] = createSignal<ReviewTarget>({ kind: "working" });
	const [targetCommit, setTargetCommit] =
		createSignal<ReviewCommitSummary | null>(null);
	const targetKey = (forTarget?: ReviewTarget): string => {
		const value = forTarget ?? target();
		switch (value.kind) {
			case "working":
				return "working";
			case "commit":
				return `commit:${value.sha}`;
			case "branch":
				return `branch:${value.base}:${value.head}`;
		}
	};
	const [files] = createResource(target, (value) =>
		loadReviewFiles(undefined, value),
	);
	const [allFiles] = createResource(() => listRepoFiles());
	const [commitPickerOpen, setCommitPickerOpen] = createSignal(false);
	// The target picker's second level: choosing a different base branch
	// for the branch-diff target.
	const [pickingBranchBase, setPickingBranchBase] = createSignal(false);
	const [targetNotice, setTargetNotice] = createSignal("");
	let targetNoticeTimeout: ReturnType<typeof setTimeout> | undefined;
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

	function resetPatchHorizontalScroll(ref: PatchScrollRef): void {
		if (typeof ref.scrollLeft === "number") ref.scrollLeft = 0;
	}

	function applyPendingPatchScrollReset(ref: PatchScrollRef): boolean {
		if (!pendingPatchScrollReset) return false;
		if (ref.scrollTo) ref.scrollTo({ x: 0, y: 0 });
		else if (typeof ref.scrollTop === "number") ref.scrollTop = 0;
		resetPatchHorizontalScroll(ref);
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

	// ── Review targets ───────────────────────────────────────

	// Drafts are per-target and preserved across switches; switching away
	// stashes the current maps, switching back restores them.
	const draftStash = new Map<string, ReviewDraftState>();

	function showTargetNotice(text: string, durationMs = 5000): void {
		if (targetNoticeTimeout) clearTimeout(targetNoticeTimeout);
		setTargetNotice(text);
		targetNoticeTimeout = setTimeout(() => {
			targetNoticeTimeout = undefined;
			setTargetNotice("");
		}, durationMs);
	}
	onCleanup(() => {
		if (targetNoticeTimeout) clearTimeout(targetNoticeTimeout);
	});

	function stashedDraftCount(key: string): number {
		if (key === targetKey()) return totalDraftNotes();
		const stashed = draftStash.get(key);
		return stashed ? countDraftNotes(stashed) : 0;
	}

	function switchTarget(
		next: ReviewTarget,
		commit: ReviewCommitSummary | null,
	): void {
		const currentKey = targetKey();
		const nextKey = targetKey(next);
		if (nextKey === currentKey) return;
		draftStash.set(currentKey, {
			fileNotes: fileNotes(),
			rangeNotes: rangeNotes(),
		});
		const restored = draftStash.get(nextKey);
		setFileNotes(restored?.fileNotes ?? new Map());
		setRangeNotes(restored?.rangeNotes ?? new Map());
		setTargetCommit(commit);
		setRangeAnchor(null);
		setEditingRange(null);
		setEditingFileNoteKey(null);
		setEditorOpen(false);
		setViewingFilePath(null);
		setSelectedIndex(0);
		setTreeFocusedPath(null);
		setMode("tree");
		setTarget(next);
	}

	function cycleTarget(): void {
		if (target().kind === "working") {
			const head = resolveCommit(undefined, "HEAD");
			if (!head) {
				props.toast({
					title: "No commits",
					subtitle: "This repository has no commits to review.",
					variant: "warning",
				});
				return;
			}
			const treeWasDirty = reviewFiles().length > 0;
			switchTarget({ kind: "commit", sha: head.sha }, head);
			if (treeWasDirty) {
				showTargetNotice("Showing HEAD (working tree has changes).", 2000);
			}
		} else {
			switchTarget({ kind: "working" }, null);
		}
	}

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
		// Commit targets only offer the commit's own files: the filesystem
		// listing (and the read-only file view it opens) reflects the
		// working tree, not the commit snapshot.
		const paths = Array.from(
			new Set([
				...(target().kind === "working" ? (allFiles() ?? []) : []),
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
	/** Pin head + merge-base and switch to a branch target vs `base`. */
	function switchToBranchTarget(base: string): void {
		const branchName = getCurrentBranch(undefined);
		const head = resolveCommit(undefined, "HEAD");
		const mergeBase = head ? getMergeBase(undefined, base, head.sha) : null;
		if (!branchName || !head || !mergeBase) {
			props.toast({
				title: "No common history",
				subtitle: `Cannot diff the current branch against ${base}.`,
				variant: "warning",
			});
			return;
		}
		switchTarget(
			{ kind: "branch", base, head: head.sha, mergeBase },
			{
				sha: head.sha,
				shortSha: head.shortSha,
				subject: `${branchName} vs ${base}`,
				relativeTime: head.relativeTime,
			},
		);
	}

	/**
	 * Git state snapshot for the target picker, captured once when the
	 * picker opens. Keeping the blocking git spawns out of the options
	 * memo means only the (cheap, reactive) draft-count decoration can
	 * re-run it.
	 */
	type PickerGitState = {
		commits: ReviewCommitSummary[];
		branchName: string | null;
		branchBase: string | null;
		branchHead: ReviewCommitSummary | null;
		branchMergeBase: string | null;
		localBranches: ReviewBranchSummary[];
	};
	const [pickerGitState, setPickerGitState] =
		createSignal<PickerGitState | null>(null);

	function openCommitPicker(): void {
		const branchName = getCurrentBranch(undefined);
		const branchBase = resolveDefaultBranchBase(undefined);
		const branchHead = branchBase ? resolveCommit(undefined, "HEAD") : null;
		setPickerGitState({
			commits: listRecentCommits(undefined),
			branchName,
			branchBase,
			branchHead,
			branchMergeBase:
				branchBase && branchHead
					? getMergeBase(undefined, branchBase, branchHead.sha)
					: null,
			localBranches: branchName ? listLocalBranches(undefined) : [],
		});
		setPickingBranchBase(false);
		setCommitPickerOpen(true);
	}

	const commitPickerOptions = createMemo<PickerOption[]>(() => {
		if (!commitPickerOpen()) return [];
		const git = pickerGitState();
		if (!git) return [];
		// Second level: choose the base branch for the branch diff.
		if (pickingBranchBase()) {
			return git.localBranches.map((branch) => ({
				name: branch.name,
				description: branch.relativeTime,
				action: (ctx) => {
					ctx.dismiss();
					setCommitPickerOpen(false);
					setPickingBranchBase(false);
					switchToBranchTarget(branch.name);
				},
			}));
		}
		const options: PickerOption[] = [];
		const workingDrafts = stashedDraftCount("working");
		// Working tree pinned as the first row: the picker is the single
		// source of truth for target selection.
		options.push({
			name: `${workingDrafts > 0 ? `${CIRCLE_FILLED} ` : ""}working tree`,
			description:
				workingDrafts > 0
					? `${workingDrafts} note${workingDrafts === 1 ? "" : "s"} drafted`
					: "uncommitted changes",
			nameColor: theme.textPrimary,
			action: (ctx) => {
				ctx.dismiss();
				setCommitPickerOpen(false);
				switchTarget({ kind: "working" }, null);
			},
		});
		// Branch total diff pinned second, when a base branch is resolvable.
		const { branchName, branchBase, branchHead, branchMergeBase } = git;
		if (branchName && branchBase && branchHead && branchMergeBase) {
			const key = `branch:${branchBase}:${branchHead.sha}`;
			const drafts = stashedDraftCount(key);
			options.push({
				name: `${drafts > 0 ? `${CIRCLE_FILLED} ` : ""}branch ${branchName} vs ${branchBase}`,
				description:
					drafts > 0
						? `${drafts} drafted ${MIDDLE_DOT} total branch diff`
						: "total branch diff",
				nameColor: theme.textPrimary,
				action: (ctx) => {
					ctx.dismiss();
					setCommitPickerOpen(false);
					switchToBranchTarget(branchBase);
				},
			});
		}
		// Choosing a different base swaps the picker to a branch list.
		if (branchName && git.localBranches.length > 0) {
			options.push({
				name: `branch ${branchName} vs …`,
				description: "choose a base branch",
				nameColor: theme.textPrimary,
				action: () => {
					setPickingBranchBase(true);
				},
			});
		}
		for (const commit of git.commits) {
			const drafts = stashedDraftCount(`commit:${commit.sha}`);
			options.push({
				name: `${drafts > 0 ? `${CIRCLE_FILLED} ` : ""}${commit.shortSha}  ${commit.subject}`,
				description:
					drafts > 0
						? `${drafts} drafted ${MIDDLE_DOT} ${commit.relativeTime}`
						: commit.relativeTime,
				nameColor: theme.metaText,
				action: (ctx) => {
					ctx.dismiss();
					setCommitPickerOpen(false);
					switchTarget({ kind: "commit", sha: commit.sha }, commit);
				},
			});
		}
		return options;
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
		const file = selectedFile();
		if (!anchor || !hunk || !file) return null;
		const line = getCommentableLines(hunk, anchor.side, diffView()).find(
			(candidate) => candidate.lineNumber === anchor.lineNumber,
		);
		return line
			? getCommentableLineTop(
					hunk,
					line.index,
					diffView(),
					selectedFileCommentAnnotations(),
					contentColumnsFor(file, diffView(), diffPaneWidth()),
				)
			: null;
	});
	const activeRangeLineBounds = createMemo(() => {
		const range = selectedRange();
		const anchor = rangeAnchor();
		const hunk = selectedHunk();
		const file = selectedFile();
		if (!range || !anchor || !hunk || !file) return null;
		return getVisualBoundsForRange(
			hunk,
			range,
			diffView(),
			selectedFileCommentAnnotations(),
			contentColumnsFor(file, diffView(), diffPaneWidth()),
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

	function openFileFinder() {
		if (editorOpen() || fileFinderOpen() || commitPickerOpen()) return;
		setFileFinderOpen(true);
	}

	function selectFilePath(filePath: string) {
		setTreeFocusedPath(filePath);
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
			// The read-only viewer reads the working tree; never open it for
			// a commit target where the filesystem may have moved on.
			if (target().kind !== "working") return;
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

	/**
	 * Expanded body of a skipped (unchanged) section, rendered as a
	 * context-only hunk so it gets the same line-number gutter and
	 * wrap-aware heights as the surrounding change groups.
	 */
	function renderSkippedSectionBlock(
		file: ReviewFile,
		section: ReviewSkippedSection,
	) {
		const hunk = skippedSectionToHunk(section);
		return (
			<box paddingLeft={2}>
				<ReviewDiffBlock
					hunk={hunk}
					view={diffView()}
					filetype={file.filetype}
					lineNumberWidth={lineNumberWidthForFile(file)}
					contentColumns={contentColumnsFor(file, diffView(), diffPaneWidth())}
				/>
			</box>
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
				contentColumnsFor(file, diffView(), diffPaneWidth()),
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
				<box position="relative" paddingLeft={2}>
					<ReviewDiffBlock
						hunk={hunk}
						view={diffView()}
						filetype={file.filetype}
						annotations={annotations()}
						activeLine={activeLine()}
						lineNumberWidth={lineNumberWidthForFile(file)}
						contentColumns={contentColumnsFor(
							file,
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
					{renderSkippedSectionBlock(file, section)}
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
				<For each={window().hunks}>
					{(hunk) => {
						const hunkIndex = () =>
							file.hunks.findIndex((candidate) => candidate.id === hunk.id);
						const section = () => getSkippedSection(file, hunkIndex());
						return (
							<>
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
												{renderSkippedSectionBlock(file, section())}
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
									{renderSkippedSectionBlock(file, section())}
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
		// A target switch may still be refetching; submitting then would
		// pair the old target's file list with the new target's notes.
		if (files.loading) return;
		const currentTarget = target();

		const submission = buildReviewSubmission(reviewFiles(), draftState());
		if (!submission) {
			props.toast({
				title: "No review notes",
				subtitle: "Add a file or line note before submitting review.",
				variant: "warning",
			});
			return;
		}

		// Amend/rebase staleness defense: line numbers only make sense
		// against the drafted revisions. A rewritten commit stops being an
		// ancestor of HEAD. Block — never silently rebind.
		const committedHead =
			currentTarget.kind === "commit"
				? currentTarget.sha
				: currentTarget.kind === "branch"
					? currentTarget.head
					: null;
		if (committedHead && !isAncestorOfHead(undefined, committedHead)) {
			props.toast({
				title: `Commit ${targetCommit()?.shortSha ?? committedHead} changed`,
				subtitle:
					"It was amended or rebased since you started drafting. Re-open the target to review the new diff.",
				variant: "error",
			});
			return;
		}
		if (currentTarget.kind === "commit") {
			submission.commit = {
				sha: currentTarget.sha,
				parentSha: resolveCommitParent(undefined, currentTarget.sha),
				subject: targetCommit()?.subject ?? "",
			};
		} else if (currentTarget.kind === "branch") {
			submission.commit = {
				sha: currentTarget.head,
				parentSha: currentTarget.mergeBase,
				subject: targetCommit()?.subject ?? "",
			};
		}

		// Submission is scoped to the current target only. Closing the
		// review destroys the per-target stash, so be honest about drafts
		// left behind on other targets.
		let otherDrafts = 0;
		for (const [key, stashed] of draftStash) {
			if (key === targetKey()) continue;
			otherDrafts += countDraftNotes(stashed);
		}
		if (otherDrafts > 0) {
			props.toast({
				title: "Review attached",
				subtitle: `${otherDrafts} draft note${otherDrafts === 1 ? "" : "s"} on other review targets ${otherDrafts === 1 ? "was" : "were"} discarded.`,
				variant: "warning",
			});
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
		if (event.button !== 0) return;
		event.preventDefault();
		event.stopPropagation();

		const range = {
			path: file.path,
			side: line.side,
			startLine: line.lineNumber,
			endLine: line.lineNumber,
		} satisfies ReviewRangeDraft;
		const editing = editingRange();
		if (editing && buildRangeNoteKey(editing) === buildRangeNoteKey(range)) {
			closeRangeNoteEditor();
			return;
		}
		if (editorOpen()) return;

		setRangeAnchor(null);
		focusDiffLine(file, hunk, line);
		void openRangeNoteEditor(file, range);
	}

	function handleReadOnlyLineMouseDown(
		filePath: string,
		line: number,
		event: TuiMouseEvent,
	) {
		if (event.button !== 0) return;
		event.preventDefault();
		event.stopPropagation();
		setViewingFileLine(line);
		const range: ReviewRangeDraft = {
			path: filePath,
			side: "additions",
			startLine: line,
			endLine: line,
		};
		const editing = viewingFileEditingRange();
		if (editing && buildRangeNoteKey(editing) === buildRangeNoteKey(range)) {
			setViewingFileEditingRange(null);
			setViewingFileEditingValue("");
			setEditorOpen(false);
			return;
		}
		if (editorOpen()) return;
		setViewingFileEditingValue(
			rangeNotes().get(buildRangeNoteKey(range))?.trim() ?? "",
		);
		setViewingFileEditingRange(range);
		setEditorOpen(true);
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => !editorOpen() && !fileFinderOpen() && !commitPickerOpen(),
		diagnosticsWhen: () =>
			!editorOpen() && !fileFinderOpen() && !commitPickerOpen(),
		commands: {
			"review.search-tree": openFileFinder,
			"review.cycle-target": cycleTarget,
			"review.pick-commit": openCommitPicker,
		},
	}));

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
		when: () =>
			!editorOpen() &&
			!fileFinderOpen() &&
			!commitPickerOpen() &&
			mode() === "patch" &&
			!viewingFilePath(),
		diagnosticsWhen: () =>
			mode() === "patch" && !fileFinderOpen() && !commitPickerOpen(),
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
		when: () =>
			!editorOpen() &&
			!fileFinderOpen() &&
			!commitPickerOpen() &&
			mode() === "patch" &&
			!!viewingFilePath(),
		diagnosticsWhen: () =>
			mode() === "patch" && !fileFinderOpen() && !commitPickerOpen(),
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
		when: () =>
			!editorOpen() &&
			mode() === "tree" &&
			!fileFinderOpen() &&
			!commitPickerOpen(),
		diagnosticsWhen: () =>
			mode() === "tree" && !fileFinderOpen() && !commitPickerOpen(),
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
					left={
						<text fg={theme.textMuted}>
							Code review {CHEVRON_RIGHT}{" "}
							<Show
								when={targetCommit()}
								fallback={
									<span style={{ fg: theme.textPrimary }}>working tree</span>
								}
							>
								{(commit) => (
									<>
										<span style={{ fg: theme.metaText }}>
											{commit().shortSha}
										</span>{" "}
										<span style={{ fg: theme.textPrimary }}>
											{commit().subject}
										</span>
									</>
								)}
							</Show>
						</text>
					}
					right={
						<text fg={theme.textMuted}>
							{targetCommit()
								? `committed ${targetCommit()?.relativeTime} · `
								: ""}
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
				<Show when={targetNotice()}>
					<box
						flexShrink={0}
						paddingX={1}
						backgroundColor={theme.bgSurface}
						width="100%"
					>
						<text fg={theme.textSecondary} bg={theme.bgSurface}>
							{targetNotice()}
						</text>
					</box>
				</Show>
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
								// Commit targets restrict the tree to the commit's
								// changes; the filesystem listing reflects the
								// working tree, not the commit snapshot.
								allFiles={target().kind === "working" ? (allFiles() ?? []) : []}
								focused={mode() === "tree"}
								editorOpen={editorOpen()}
								// Any floating picker suppresses tree navigation,
								// not just the file finder.
								finderOpen={fileFinderOpen() || commitPickerOpen()}
								focusedPath={treeFocusedPath()}
								onFocusedPathChange={(path) => {
									setTreeFocusedPath(path);
									// Sync selectedIndex for diff state
									if (path) {
										const idx = reviewFiles().findIndex((f) => f.path === path);
										if (idx >= 0) setSelectedIndex(idx);
									}
								}}
								onSelectFile={selectFilePath}
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
												onLineMouseDown={(line, event) =>
													handleReadOnlyLineMouseDown(filePath(), line, event)
												}
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
											</box>
											<Show when={sourceLabel(file())}>
												{(label) => <text fg={theme.textMuted}>{label()}</text>}
											</Show>
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
											backgroundColor={theme.bg}
											overflow="hidden"
										>
											<scrollbox
												ref={(value) => {
													if (!value) return;
													patchScrollRef = value as PatchScrollRef;
													resetPatchHorizontalScroll(patchScrollRef);
													applyPendingPatchScrollReset(patchScrollRef);
												}}
												flexGrow={1}
												scrollY
												style={scrollbarStyle()}
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
				<Show when={commitPickerOpen()}>
					<CommitPickerDialog
						options={commitPickerOptions()}
						onClose={() => {
							setCommitPickerOpen(false);
							setPickingBranchBase(false);
						}}
					/>
				</Show>
			</Show>
		</ScreenLayout>
	);
}

// ── Commit picker dialog ────────────────────────────────────

type CommitPickerDialogProps = {
	options: PickerOption[];
	onClose: () => void;
};

/**
 * Review-target picker: recent commits with the working tree pinned as
 * the first row. Capped at 20 commits by design — this is not a history
 * explorer (docs/features/code-review-commit-targets.md).
 */
function CommitPickerDialog(props: CommitPickerDialogProps) {
	const picker = createPickerManager();
	let didShow = false;

	createEffect(() => {
		if (didShow) return;
		didShow = true;
		picker.show({
			label: "Review target",
			options: props.options,
			filterable: true,
			onDismiss: props.onClose,
		});
	});

	createEffect(() => {
		picker.updateOptions(props.options);
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
						commandNamespace="review-commit-picker"
					>
						<Picker.Header />
						<Picker.Body />
						<Picker.Footer flexDirection="column">
							<KeymapHintBar borderless group="review-commit-picker" />
						</Picker.Footer>
					</Picker.Root>
				</box>
			</Dialog.Root>
		</Show>
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
	onLineMouseDown: (line: number, event: TuiMouseEvent) => void;
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
	// Content area = paneWidth minus the gutter (line number column + the
	// two-space separator). The content box is full-bleed so rows align
	// flush with the pane header above.
	const contentColumns = createMemo(() =>
		Math.max(10, props.paneWidth - lineNumberWidth() - 2),
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

			<box flexGrow={1} backgroundColor={theme.bg}>
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
						style={scrollbarStyle()}
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
												onMouseDown={(event) => {
													if (!props.interactive) return;
													props.onLineMouseDown(lineNum(), event);
												}}
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
