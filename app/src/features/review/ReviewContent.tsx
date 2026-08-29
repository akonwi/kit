import { watch } from "node:fs";
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
	untrack,
} from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { ReviewDiffView } from "../../settings";
import type { AttachmentsController } from "../../shell/attachments-controller";
import { Dialog } from "../../shell/Dialog";
import {
	getReviewDiffActiveLineId,
	getReviewDiffCommentableLines,
	type ReviewDiffAnnotationMetadata,
	ReviewDiffBlock,
	type ReviewDiffCommentableLine,
	shouldResetPatchScroll,
} from "../../shell/diff/ReviewDiffBlock";
import {
	CIRCLE_FILLED,
	MIDDLE_DOT,
	TRIANGLE_DOWN,
	TRIANGLE_RIGHT,
} from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { MessageComposer, type TextareaRef } from "../../shell/MessageComposer";
import { Picker } from "../../shell/Picker";
import { scrollbarStyle, theme } from "../../shell/theme";
import {
	WorkspacePanelHeader,
	WorkspacePanelLayout,
	WorkspaceSidebarToggle,
} from "../../shell/WorkspacePanelLayout";
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
import {
	type ReviewDraftController,
	reviewTargetKey,
} from "./draft-controller";
import { reviewDraftDetachAction } from "./draft-detach";
import { FileTreePanel } from "./FileTreePanel";
import { reconcileReviewFiles } from "./live-files";
import {
	getCurrentBranch,
	getMergeBase,
	isAncestorOfHead,
	listLocalBranches,
	listRecentCommits,
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
import { resolveReviewWorkspaceFilePath } from "./open-workspace-file";
import { reviewStatusLabel } from "./status";
import { getWorkingTreeFingerprint } from "./working-tree-fingerprint";
import { createWorkingTreeRefreshTracker } from "./working-tree-refresh";

export type ReviewContentProps = {
	repoRoot: string;
	onClose: () => void;
	onTabClose?: () => void;
	onCloseRequestReady?: (request: (() => void) | null) => void;
	attachments: AttachmentsController;
	reviewDrafts: ReviewDraftController;
	toast: (toast: ToastInput) => void;
	defaultDiffView: ReviewDiffView;
	onDiffViewChanged?: (view: ReviewDiffView) => void;
	active?: boolean;
	onFocusRequest?: () => void;
	onSubmitMessage: () => void | Promise<void>;
	onOpenFile: (path: string) => void;
	onFindFile: () => void;
};

export type ReviewMode = "tree" | "patch";

export function resolveReviewPaneVisibility(options: {
	wide: boolean;
	mode: ReviewMode;
	treeExpanded: boolean;
	editorOpen?: boolean;
}): { tree: boolean; diff: boolean } {
	const mode = options.editorOpen ? "patch" : options.mode;
	return {
		tree: options.wide ? options.treeExpanded : mode === "tree",
		diff: options.wide || mode === "patch",
	};
}

export function toggleReviewTreeState(options: {
	wide: boolean;
	mode: ReviewMode;
	treeExpanded: boolean;
}): { mode: ReviewMode; treeExpanded: boolean } {
	const expanded = resolveReviewPaneVisibility(options).tree;
	return {
		mode: expanded ? "patch" : "tree",
		treeExpanded: !expanded,
	};
}

type ReviewSide = "additions" | "deletions";
type CommentableLine = ReviewDiffCommentableLine;

const WIDE_VIEWPORT_THRESHOLD = 121;
const WORKING_TREE_REFRESH_DEBOUNCE_MS = 250;
const WORKING_TREE_REFRESH_FALLBACK_MS = 5000;
const WORKING_TREE_REFRESH_POLL_MS = 1000;
const REVIEW_TARGET_CACHE_LIMIT = 3;
const TREE_PANEL_WIDTH = 36;
const MIN_TREE_PANEL_WIDTH = 28;

const PATCH_FOCUSED_RENDER_HUNK_LIMIT = 40;
const PATCH_FOCUSED_RENDER_LINE_LIMIT = 800;
const PATCH_WINDOW_HUNK_LIMIT = 12;
const PATCH_WINDOW_LINE_LIMIT = 800;
/**
 * How close (in lines) the selection may get to a patch window's edge
 * before the window re-centers. Larger margin = fewer re-centers but
 * less lookahead context near the edges.
 */
const PATCH_WINDOW_EDGE_MARGIN = 40;

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

export function ReviewSkippedSectionRow(props: {
	section: ReviewSkippedSection;
	interactive: boolean;
	selected: boolean;
	expanded: boolean;
	onActivate: () => void;
}) {
	const [hovered, setHovered] = createSignal(false);
	const highlighted = () => props.selected || (props.interactive && hovered());
	return (
		<box
			id={`review-skipped-section-${props.section.id}`}
			paddingX={1}
			paddingY={0}
			flexDirection="row"
			justifyContent="space-between"
			backgroundColor={highlighted() ? theme.bgMuted : theme.bgSurface}
			onMouseOver={() => {
				if (props.interactive) setHovered(true);
			}}
			onMouseOut={() => setHovered(false)}
			onMouseDown={(event) => {
				if (!props.interactive || event.button !== 0) return;
				event.preventDefault();
				event.stopPropagation();
				props.onActivate();
			}}
		>
			<text fg={props.selected ? theme.metaText : theme.textMuted}>
				{props.expanded ? TRIANGLE_DOWN : TRIANGLE_RIGHT}{" "}
				{props.section.lineCount} unchanged line
				{props.section.lineCount === 1 ? "" : "s"}{" "}
				{props.expanded ? "shown" : "hidden"}
			</text>
			<text fg={props.selected ? theme.textSecondary : theme.textPlaceholder}>
				{skippedSectionLineLabel(props.section)}
				{props.interactive && props.selected
					? ` · Space ${props.expanded ? "collapse" : "expand"}`
					: ""}
			</text>
		</box>
	);
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
const lineNumberWidthCache = new WeakMap<ReviewFile, number>();

function lineNumberWidthForFile(file: ReviewFile): number {
	const cached = lineNumberWidthCache.get(file);
	if (cached !== undefined) return cached;
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
	lineNumberWidthCache.set(file, width);
	return width;
}

// Patch content is full-bleed. Only reserve the overlaid scrollbar column.
const PATCH_CONTENT_PADDING = 0;
const PATCH_SCROLLBAR_COLUMNS = 1;

function unifiedContentColumns(lnw: number, diffPaneWidth: number): number {
	// Unified row: [lnw][space][lnw][sign][space]
	const gutterCols = 2 * lnw + 3;
	return Math.max(
		1,
		diffPaneWidth -
			PATCH_CONTENT_PADDING -
			PATCH_SCROLLBAR_COLUMNS -
			gutterCols,
	);
}

function splitContentColumns(lnw: number, diffPaneWidth: number): number {
	const inner = diffPaneWidth - PATCH_CONTENT_PADDING - PATCH_SCROLLBAR_COLUMNS;
	const halfWidth = Math.floor(inner / 2);
	// Split cell: [lnw][sign][space]
	return Math.max(1, halfWidth - lnw - 2);
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
	let disposed = false;
	onCleanup(() => {
		disposed = true;
	});
	const active = () => props.active !== false;
	const repoRoot = props.repoRoot;
	const draftToken = props.reviewDrafts.currentToken();
	const savedTarget = props.reviewDrafts.getLastTarget(draftToken, repoRoot);
	const savedCommit =
		savedTarget.kind === "working"
			? null
			: resolveCommit(
					repoRoot,
					savedTarget.kind === "commit" ? savedTarget.sha : savedTarget.head,
				);
	const mergeBaseResolves =
		savedTarget.kind !== "branch" ||
		resolveCommit(repoRoot, savedTarget.mergeBase) !== null;
	const initialTarget =
		savedTarget.kind === "working" || (savedCommit && mergeBaseResolves)
			? savedTarget
			: ({ kind: "working" } satisfies ReviewTarget);
	if (initialTarget.kind === "working" && savedTarget.kind !== "working") {
		// A temporarily unreachable commit must not destroy authored notes.
		// Fall back to working while keeping its draft inert in memory.
		props.reviewDrafts.setLastTarget(draftToken, repoRoot, initialTarget);
	}
	const initialCommit =
		initialTarget.kind === "working" || !savedCommit
			? null
			: initialTarget.kind === "branch"
				? {
						...savedCommit,
						subject: `${getCurrentBranch(repoRoot) ?? savedCommit.shortSha} vs ${initialTarget.base}`,
					}
				: savedCommit;
	const initialDraft = props.reviewDrafts.getDraft(
		draftToken,
		repoRoot,
		initialTarget,
	);
	// What the review is diffing: working tree (default), one commit, or a
	// branch's total diff. See docs/features/code-review-commit-targets.md.
	const [target, setTarget] = createSignal<ReviewTarget>(initialTarget);
	const [targetCommit, setTargetCommit] =
		createSignal<ReviewCommitSummary | null>(initialCommit);
	const [editorOpen, setEditorOpen] = createSignal(false);
	const [submittingReview, setSubmittingReview] = createSignal(false);
	const [staleReviewFileIds, setStaleReviewFileIds] = createSignal<Set<string>>(
		new Set(),
	);
	const targetKey = (forTarget?: ReviewTarget): string =>
		reviewTargetKey(forTarget ?? target());
	function fileHasDraftNotes(file: ReviewFile): boolean {
		if (fileNotes().get(file.noteKey)?.trim()) return true;
		for (const [key, value] of rangeNotes()) {
			if (!value.trim()) continue;
			if (parseRangeNoteKey(key)?.path === file.path) return true;
		}
		return false;
	}
	type LoadedReviewFiles = {
		targetKey: string;
		files: ReviewFile[];
		changed: boolean;
	};
	const loadedFilesByTarget = new Map<string, ReviewFile[]>();
	function cacheLoadedFiles(key: string, files: ReviewFile[]): void {
		loadedFilesByTarget.delete(key);
		loadedFilesByTarget.set(key, files);
		while (loadedFilesByTarget.size > REVIEW_TARGET_CACHE_LIMIT) {
			const oldestKey = loadedFilesByTarget.keys().next().value;
			if (oldestKey === undefined) break;
			loadedFilesByTarget.delete(oldestKey);
		}
	}
	const refreshTracker = createWorkingTreeRefreshTracker();
	const [loadedReviewFiles, { refetch: refetchReviewFiles }] = createResource(
		target,
		async (value): Promise<LoadedReviewFiles> => {
			const key = reviewTargetKey(value);
			const current = loadedFilesByTarget.get(key);
			// Snapshot before reading the tree: a publish is only as fresh as
			// the last observation preceding the git reads.
			const fingerprintAtStart =
				value.kind === "working" ? refreshTracker.beginRefresh() : undefined;
			try {
				// Pin git to the pane's repo root: the watcher and fingerprint
				// poll observe repoRoot, so diff loading must read the same repo
				// even if the process cwd changes mid-session.
				const next = await loadReviewFiles(repoRoot, value);
				if (disposed) {
					return { targetKey: key, files: current ?? [], changed: false };
				}
				// Do not publish a live result underneath an editor that opened
				// while the Git commands were in flight. The fingerprint poll
				// retries because a dropped fetch never marks itself published.
				if (current && value.kind === "working" && editorOpen()) {
					return { targetKey: key, files: current, changed: false };
				}
				const staleIds = new Set<string>();
				const stable = reconcileReviewFiles(
					current,
					next,
					value.kind === "working"
						? (file) => {
								const preserve = fileHasDraftNotes(file);
								if (preserve) staleIds.add(file.id);
								return preserve;
							}
						: undefined,
				);
				setStaleReviewFileIds(staleIds);
				cacheLoadedFiles(key, stable);
				// Only a successful working-tree read marks the tracker published;
				// dropped and failed paths leave it behind so the fingerprint poll
				// keeps retrying.
				if (value.kind === "working") {
					refreshTracker.markPublished(fingerprintAtStart);
				}
				return {
					targetKey: key,
					files: stable,
					changed: stable !== current,
				};
			} catch (error) {
				if (!current && !disposed) {
					props.toast({
						title: "Failed to load code review",
						subtitle: error instanceof Error ? error.message : String(error),
						variant: "error",
					});
				}
				const fallback = current ?? [];
				if (!disposed) cacheLoadedFiles(key, fallback);
				return { targetKey: key, files: fallback, changed: false };
			}
		},
	);
	const currentLoadedReview = createMemo(() => {
		const key = targetKey();
		const current = loadedReviewFiles();
		if (current?.targetKey === key) return current;
		const latest = loadedReviewFiles.latest;
		return latest?.targetKey === key ? latest : undefined;
	});
	const files = () => currentLoadedReview()?.files;
	const filesLoading = () => currentLoadedReview() === undefined;
	const [commitPickerOpen, setCommitPickerOpen] = createSignal(false);
	// The target picker's second level: choosing a different base branch
	// for the branch-diff target.
	const [pickingBranchBase, setPickingBranchBase] = createSignal(false);
	const [targetNotice, setTargetNotice] = createSignal("");
	let targetNoticeTimeout: ReturnType<typeof setTimeout> | undefined;
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [mode, setMode] = createSignal<ReviewMode>("tree");
	const [treeExpanded, setTreeExpanded] = createSignal(true);
	const [contentWidth, setContentWidth] = createSignal(120);

	function showChangesTree(): void {
		setTreeExpanded(true);
		setMode("tree");
	}

	function toggleChangesTree(): void {
		if (editorOpen() || commitPickerOpen()) return;
		props.onFocusRequest?.();
		const next = toggleReviewTreeState({
			wide: isWide(),
			mode: mode(),
			treeExpanded: treeExpanded(),
		});
		setTreeExpanded(next.treeExpanded);
		setMode(next.mode);
	}
	const [treeFocusedPath, setTreeFocusedPath] = createSignal<string | null>(
		null,
	);
	createEffect(() => {
		if (active()) return;
		setCommitPickerOpen(false);
		setPickingBranchBase(false);
	});
	const [fileNotes, setFileNotes] = createSignal<Map<string, string>>(
		initialDraft.fileNotes,
	);
	const [rangeNotes, setRangeNotes] = createSignal<Map<string, string>>(
		initialDraft.rangeNotes,
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
	let workingTreeRefreshInFlight = false;
	let workingTreeRefreshPending = false;
	let workingTreeRefreshTimeout: ReturnType<typeof setTimeout> | undefined;
	function scheduleWorkingTreeRefresh(
		delay = WORKING_TREE_REFRESH_DEBOUNCE_MS,
	): void {
		if (disposed) return;
		if (workingTreeRefreshTimeout) clearTimeout(workingTreeRefreshTimeout);
		workingTreeRefreshTimeout = setTimeout(() => {
			workingTreeRefreshTimeout = undefined;
			void refreshWorkingTree();
		}, delay);
	}
	async function refreshWorkingTree(): Promise<void> {
		if (disposed || target().kind !== "working" || editorOpen()) return;
		if (workingTreeRefreshInFlight) {
			workingTreeRefreshPending = true;
			return;
		}
		if (loadedReviewFiles.loading) {
			scheduleWorkingTreeRefresh();
			return;
		}
		workingTreeRefreshInFlight = true;
		try {
			await refetchReviewFiles();
		} catch {
			// Working-tree refresh is best-effort; keep the last good diff and retry.
		} finally {
			workingTreeRefreshInFlight = false;
			if (!disposed && workingTreeRefreshPending) {
				workingTreeRefreshPending = false;
				scheduleWorkingTreeRefresh();
			}
		}
	}
	let workingTreeWatcher: ReturnType<typeof watch> | undefined;
	let workingTreeWatcherActive = false;
	let fingerprintCheckInFlight = false;
	async function checkWorkingTreeFingerprint(): Promise<void> {
		if (disposed || fingerprintCheckInFlight || target().kind !== "working") {
			return;
		}
		fingerprintCheckInFlight = true;
		try {
			const next = await getWorkingTreeFingerprint(repoRoot);
			if (refreshTracker.observe(next)) {
				scheduleWorkingTreeRefresh(0);
			}
		} catch {
			// The watcher remains primary; a later fingerprint check can recover.
		} finally {
			fingerprintCheckInFlight = false;
		}
	}
	void checkWorkingTreeFingerprint();
	try {
		workingTreeWatcher = watch(
			repoRoot,
			{ recursive: true },
			(_event, filename) => {
				const changedPath = String(filename ?? "").replaceAll("\\", "/");
				if (
					changedPath === "node_modules" ||
					changedPath.startsWith("node_modules/") ||
					changedPath.startsWith(".git/objects/")
				) {
					return;
				}
				scheduleWorkingTreeRefresh();
			},
		);
		workingTreeWatcherActive = true;
		workingTreeWatcher.on("error", () => {
			workingTreeWatcherActive = false;
			workingTreeWatcher?.close();
			workingTreeWatcher = undefined;
		});
	} catch {
		// Recursive watching is not available on every platform; poll below.
	}
	let workingTreeFallbackElapsed = 0;
	const workingTreeRefreshTimer = setInterval(() => {
		workingTreeFallbackElapsed += WORKING_TREE_REFRESH_POLL_MS;
		if (
			workingTreeWatcherActive &&
			workingTreeFallbackElapsed < WORKING_TREE_REFRESH_FALLBACK_MS
		) {
			return;
		}
		workingTreeFallbackElapsed = 0;
		void checkWorkingTreeFingerprint();
	}, WORKING_TREE_REFRESH_POLL_MS);
	let editorWasOpen = editorOpen();
	createEffect(() => {
		const open = editorOpen();
		if (editorWasOpen && !open) scheduleWorkingTreeRefresh(0);
		editorWasOpen = open;
	});
	onCleanup(() => {
		workingTreeWatcher?.close();
		clearInterval(workingTreeRefreshTimer);
		if (workingTreeRefreshTimeout) clearTimeout(workingTreeRefreshTimeout);
	});
	onCleanup(
		props.reviewDrafts.subscribe((event) => {
			if (event.type === "reset") {
				setFileNotes(new Map());
				setRangeNotes(new Map());
				setStaleReviewFileIds(new Set<string>());
				setEditorOpen(false);
				return;
			}
			if (
				event.token.sessionId !== draftToken.sessionId ||
				event.token.generation !== draftToken.generation ||
				event.repoRoot !== repoRoot ||
				event.targetKey !== targetKey()
			) {
				return;
			}
			if (event.type === "consumed") {
				setFileNotes(new Map(event.state.fileNotes));
				setRangeNotes(new Map(event.state.rangeNotes));
				scheduleWorkingTreeRefresh(0);
				return;
			}
			setFileNotes(new Map());
			setRangeNotes(new Map());
			setStaleReviewFileIds(new Set<string>());
			setRangeAnchor(null);
			setEditingRange(null);
			setEditingFileNoteKey(null);
			setEditorOpen(false);
			scheduleWorkingTreeRefresh(0);
		}),
	);
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
	const paneVisibility = createMemo(() =>
		resolveReviewPaneVisibility({
			wide: isWide(),
			mode: mode(),
			treeExpanded: treeExpanded(),
			editorOpen: editorOpen(),
		}),
	);
	const treePanelWidth = createMemo(() =>
		Math.max(
			MIN_TREE_PANEL_WIDTH,
			Math.min(TREE_PANEL_WIDTH, Math.floor(contentWidth() * 0.35)),
		),
	);
	const diffPaneWidth = createMemo(() =>
		isWide() && paneVisibility().tree
			? Math.max(0, contentWidth() - treePanelWidth())
			: contentWidth(),
	);

	let patchCursorScrollTimeout: ReturnType<typeof setTimeout> | undefined;

	const reviewFiles = createMemo(() => files() ?? []);
	const draftState = createMemo<ReviewDraftState>(() => ({
		fileNotes: fileNotes(),
		rangeNotes: rangeNotes(),
	}));
	const totalDraftNotes = createMemo(() => countDraftNotes(draftState()));

	// ── Review targets ───────────────────────────────────────

	// Drafts are per-target and live in the active session's in-memory
	// controller, so closing and reopening review does not discard them.
	function saveCurrentDraft(state = draftState()): void {
		props.reviewDrafts.saveDraft(draftToken, repoRoot, target(), state);
	}
	function updateFileNotes(
		update: (current: Map<string, string>) => Map<string, string>,
	): void {
		setFileNotes((current) => {
			const next = update(current);
			saveCurrentDraft({ fileNotes: next, rangeNotes: rangeNotes() });
			scheduleWorkingTreeRefresh(0);
			return next;
		});
	}
	function updateRangeNotes(
		update: (current: Map<string, string>) => Map<string, string>,
	): void {
		setRangeNotes((current) => {
			const next = update(current);
			saveCurrentDraft({ fileNotes: fileNotes(), rangeNotes: next });
			scheduleWorkingTreeRefresh(0);
			return next;
		});
	}
	onCleanup(() => saveCurrentDraft());

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
		return props.reviewDrafts.countDraftForKey(draftToken, repoRoot, key);
	}

	function switchTarget(
		next: ReviewTarget,
		commit: ReviewCommitSummary | null,
	): void {
		const currentKey = targetKey();
		const nextKey = targetKey(next);
		if (nextKey === currentKey) return;
		saveCurrentDraft();
		const restored = props.reviewDrafts.getDraft(draftToken, repoRoot, next);
		setFileNotes(restored.fileNotes);
		setRangeNotes(restored.rangeNotes);
		props.reviewDrafts.setLastTarget(draftToken, repoRoot, next);
		setTargetCommit(commit);
		setRangeAnchor(null);
		setEditingRange(null);
		setEditingFileNoteKey(null);
		setEditorOpen(false);
		setSelectedIndex(0);
		setTreeFocusedPath(null);
		showChangesTree();
		setTarget(next);
	}

	function cycleTarget(): void {
		if (target().kind === "working") {
			const head = resolveCommit(repoRoot, "HEAD");
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
	const reviewFilePaths = createMemo(() => new Set(reviewFilesByPath().keys()));
	const selectedFile = createMemo(() => {
		const focused = treeFocusedPath();
		if (mode() === "tree" && focused) {
			return reviewFilesByPath().get(focused) ?? null;
		}
		return reviewFiles()[selectedIndex()] ?? null;
	});
	/** Pin head + merge-base and switch to a branch target vs `base`. */
	function switchToBranchTarget(base: string): void {
		const branchName = getCurrentBranch(repoRoot);
		const head = resolveCommit(repoRoot, "HEAD");
		const mergeBase = head ? getMergeBase(repoRoot, base, head.sha) : null;
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
		const branchName = getCurrentBranch(repoRoot);
		const branchBase = resolveDefaultBranchBase(repoRoot);
		const branchHead = branchBase ? resolveCommit(repoRoot, "HEAD") : null;
		setPickerGitState({
			commits: listRecentCommits(repoRoot),
			branchName,
			branchBase,
			branchHead,
			branchMergeBase:
				branchBase && branchHead
					? getMergeBase(repoRoot, branchBase, branchHead.sha)
					: null,
			localBranches: branchName ? listLocalBranches(repoRoot) : [],
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
			const key = reviewTargetKey({
				kind: "branch",
				base: branchBase,
				head: branchHead.sha,
				mergeBase: branchMergeBase,
			});
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
	createEffect(() => {
		const list = reviewFiles();
		if (selectedIndex() >= list.length) {
			setSelectedIndex(Math.max(0, list.length - 1));
		}
	});

	// Windowed-hunk cache entries root their source hunks (and lazily
	// materialized lines); drop them wholesale whenever the diff reloads.
	createEffect(() => {
		reviewFiles();
		patchWindowCache.clear();
	});

	createEffect(() => {
		const list = reviewFiles();
		if (list.length === 0) {
			setSelectedSectionIds(new Map<string, string>());
			setExpandedSectionIds(new Set<string>());
			showChangesTree();
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

	function openCurrentFileInWorkspace(): void {
		const path = resolveReviewWorkspaceFilePath({
			mode: mode(),
			focusedPath: treeFocusedPath(),
			changedFilePaths: reviewFilePaths(),
			selectedPath: selectedFile()?.path ?? null,
		});
		if (path) props.onOpenFile(path);
	}

	function selectFilePath(filePath: string) {
		if (editorOpen()) {
			if (editingRange()) saveRangeNoteEditor();
			else if (editingFileNoteKey()) saveFileNoteEditor();
			else setEditorOpen(false);
			setRangeAnchor(null);
		}
		setTreeFocusedPath(filePath);
		const file = reviewFilesByPath().get(filePath);
		if (!file) return;
		const index = reviewFiles().indexOf(file);
		if (index >= 0) setSelectedIndex(index);
		pendingPatchOpenReset = true;
		setMode("patch");
		if (file.hunks.length > 0) setSelectedHunkIndex(file.id, 0);
	}

	function renderRawDiffBlock(rawPatch: string, filetype?: string) {
		return (
			<ReviewDiffBlock
				rawPatch={rawPatch}
				view={diffView()}
				filetype={filetype}
				contentColumns={() => unifiedContentColumns(1, diffPaneWidth())}
				contentRightInset={PATCH_SCROLLBAR_COLUMNS}
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
			<box width="100%">
				<ReviewDiffBlock
					hunk={hunk}
					view={diffView()}
					filetype={file.filetype}
					lineNumberWidth={lineNumberWidthForFile(file)}
					contentColumns={() =>
						contentColumnsFor(file, diffView(), diffPaneWidth())
					}
					contentRightInset={PATCH_SCROLLBAR_COLUMNS}
				/>
			</box>
		);
	}

	function renderSkippedSectionRow(
		file: ReviewFile,
		section: ReviewSkippedSection,
		options: {
			interactive: boolean;
			selected: () => boolean;
			expanded: () => boolean;
		},
	) {
		return (
			<ReviewSkippedSectionRow
				section={section}
				interactive={options.interactive}
				selected={options.selected()}
				expanded={options.expanded()}
				onActivate={() => {
					props.onFocusRequest?.();
					focusSkippedSection(file, section);
					toggleExpandedContext(section.id);
				}}
			/>
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
						focused={active()}
						showCursor={active()}
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
		const activeLine = () => {
			const current = cursor();
			if (!interactive || current?.hunk.id !== hunk.id) return undefined;
			return current.line;
		};
		const lineMarker = (line: ReviewDiffCommentableLine) => {
			const current = cursor();
			if (!interactive || current?.hunk.id !== hunk.id) return undefined;
			const anchor = rangeAnchor();
			if (anchor?.side === line.side && anchor.lineNumber === line.lineNumber) {
				return "anchor" as const;
			}
			const range = selectedRange();
			if (!range || range.path !== file.path || range.side !== line.side) {
				return undefined;
			}
			const startLine = Math.min(range.startLine, range.endLine);
			const endLine = Math.max(range.startLine, range.endLine);
			return line.lineNumber >= startLine && line.lineNumber <= endLine
				? ("range" as const)
				: undefined;
		};
		return (
			<box flexDirection="column" gap={0} width="100%">
				<ReviewDiffBlock
					hunk={hunk}
					view={diffView()}
					filetype={file.filetype}
					annotations={annotations()}
					activeLine={activeLine()}
					lineMarker={lineMarker}
					lineNumberWidth={lineNumberWidthForFile(file)}
					contentColumns={() =>
						contentColumnsFor(file, diffView(), diffPaneWidth())
					}
					contentRightInset={PATCH_SCROLLBAR_COLUMNS}
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
			</box>
		);
	}

	function getRenderableLineCount(hunk: ReviewHunk): number {
		return hunk.lines.length;
	}

	/**
	 * Cache of windowed hunks keyed by hunk id. A windowed hunk is a
	 * fresh object; <For> keys by reference, so returning a new one per
	 * keystroke would tear down and rebuild the focused block's rows
	 * (syntax highlighting, wrap measurement) on every selection move.
	 * The window is kept until the selection nears its edge (hysteresis)
	 * and the same object is reused while the bounds are unchanged.
	 */
	const patchWindowCache = new Map<
		string,
		{ source: ReviewHunk; start: number; end: number; windowed: ReviewHunk }
	>();

	function getPatchWindowHunk(
		hunk: ReviewHunk,
		forceWindow: boolean,
	): ReviewHunk {
		if (!forceWindow || hunk.patchLineCount <= PATCH_WINDOW_LINE_LIMIT)
			return hunk;
		const sourceOffset = hunk.lineIndexOffset ?? 0;
		// selectedLineIndices stores positions in the *commentable-lines*
		// array (context lines filtered out), not hunk.lines. Map to the
		// selected line's true hunk.lines position via its `.index` before
		// comparing against window bounds — otherwise interleaved context
		// drift lets the cursor walk outside the frozen window unseen.
		const commentable = getCommentableLines(
			hunk,
			rangeAnchor()?.side,
			diffView(),
		);
		const storedIndex = selectedLineIndices().get(hunk.id) ?? 0;
		const clampedIndex = Math.max(
			0,
			Math.min(commentable.length - 1, storedIndex),
		);
		const relativeIndex = Math.max(
			0,
			(commentable[clampedIndex]?.index ?? 0) - sourceOffset,
		);

		const cached = patchWindowCache.get(hunk.id);
		if (cached && cached.source === hunk) {
			// Keep the current window while the selection stays comfortably
			// inside it; only re-center when it approaches an edge.
			const safeStart =
				cached.start === 0 ? 0 : cached.start + PATCH_WINDOW_EDGE_MARGIN;
			const safeEnd =
				cached.end >= hunk.lines.length
					? cached.end
					: cached.end - PATCH_WINDOW_EDGE_MARGIN;
			if (relativeIndex >= safeStart && relativeIndex < safeEnd) {
				return cached.windowed;
			}
		}

		const halfWindow = Math.floor(PATCH_WINDOW_LINE_LIMIT / 2);
		const maxStart = Math.max(0, hunk.lines.length - PATCH_WINDOW_LINE_LIMIT);
		const start = Math.max(0, Math.min(maxStart, relativeIndex - halfWindow));
		const end = Math.min(hunk.lines.length, start + PATCH_WINDOW_LINE_LIMIT);
		if (cached && cached.source === hunk && cached.start === start) {
			return cached.windowed;
		}
		const lines = hunk.lines.slice(start, end);
		const windowed: ReviewHunk = {
			...hunk,
			lines,
			lineIndexOffset: sourceOffset + start,
			lineWindow: { start, end, total: hunk.lines.length },
			changeCount: lines.filter((line) => line.kind !== "context").length,
			patchLineCount: lines.length,
		};
		patchWindowCache.set(hunk.id, { source: hunk, start, end, windowed });
		return windowed;
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
				{renderSkippedSectionRow(file, section, {
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
			<box flexDirection="column" gap={0} width="100%">
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
			<box flexDirection="column" gap={0} width="100%">
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
											{renderSkippedSectionRow(file, section(), {
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
								{renderSkippedSectionRow(file, section(), {
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
		// The editor lives in the diff pane. Focus that pane first so opening a
		// note from the tree—or resizing narrow while editing—cannot hide it.
		setMode("patch");
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
		updateFileNotes((prev) => setMapValue(prev, key, editingFileNoteValue()));
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
		updateRangeNotes((prev) =>
			setMapValue(prev, buildRangeNoteKey(range), editingRangeValue()),
		);
		closeRangeNoteEditor();
	}

	function clearSelectedFileNote() {
		const file = selectedFile();
		if (!file) return;
		updateFileNotes((prev) => setMapValue(prev, file.noteKey, ""));
	}

	function clearSelectedRangeNote() {
		const range = selectedRange();
		if (!range) return;
		updateRangeNotes((prev) => setMapValue(prev, buildRangeNoteKey(range), ""));
	}

	function currentDraftAttachment(): CodeReviewAttachment | null {
		const attachment = props.attachments
			.attachments()
			.find((candidate) => candidate.id === "code-review");
		return attachment instanceof CodeReviewAttachment ? attachment : null;
	}

	type QueueDraftResult = "queued" | "empty" | "blocked";

	type QueueDraftOptions = {
		showEmptyWarning?: boolean;
		showBlockedWarning?: boolean;
	};

	function hideProjectedDraft(): void {
		const attached = untrack(currentDraftAttachment);
		if (attached) props.attachments.detach(attached.id, "pending");
	}

	function queueCurrentDraft(
		options: QueueDraftOptions = {},
	): QueueDraftResult {
		const currentTarget = target();
		const currentKey = targetKey(currentTarget);
		if (submittingReview()) {
			hideProjectedDraft();
			if (options.showBlockedWarning) {
				showTargetNotice("Review submission is in progress.", 1500);
			}
			return "blocked";
		}
		if (totalDraftNotes() === 0) {
			hideProjectedDraft();
			if (options.showEmptyWarning) {
				props.toast({
					title: "No review notes",
					subtitle: "Add a file or line note before submitting review.",
					variant: "warning",
				});
			}
			return "empty";
		}

		// A target switch may still be refetching; queueing then would pair
		// the old target's file list with the new target's notes. Hide the stale
		// projection until the new target can be attached safely.
		if (filesLoading()) {
			hideProjectedDraft();
			if (options.showBlockedWarning) {
				showTargetNotice("Review is still loading.", 1500);
			}
			return "blocked";
		}
		const submittedDraft = draftState();
		const submission = buildReviewSubmission(reviewFiles(), submittedDraft);
		if (!submission) return "blocked";
		saveCurrentDraft(submittedDraft);

		// Amend/rebase staleness defense: line numbers only make sense
		// against the drafted revisions. A rewritten commit stops being an
		// ancestor of HEAD. Block — never silently rebind.
		const committedHead =
			currentTarget.kind === "commit"
				? currentTarget.sha
				: currentTarget.kind === "branch"
					? currentTarget.head
					: null;
		if (committedHead && !isAncestorOfHead(repoRoot, committedHead)) {
			hideProjectedDraft();
			if (options.showBlockedWarning) {
				props.toast({
					title: `Commit ${targetCommit()?.shortSha ?? committedHead} changed`,
					subtitle:
						"It was amended or rebased since you started drafting. Re-open the target to review the new diff.",
					variant: "error",
				});
			}
			return "blocked";
		}
		if (currentTarget.kind === "commit") {
			submission.commit = {
				sha: currentTarget.sha,
				parentSha: resolveCommitParent(repoRoot, currentTarget.sha),
				subject: targetCommit()?.subject ?? "",
			};
		} else if (currentTarget.kind === "branch") {
			submission.commit = {
				sha: currentTarget.head,
				parentSha: currentTarget.mergeBase,
				subject: targetCommit()?.subject ?? "",
			};
		}

		props.attachments.attach(
			new CodeReviewAttachment("code-review", submission, {
				repoRoot,
				targetKey: currentKey,
				onDetach: (reason) => {
					const action = reviewDraftDetachAction(reason);
					if (action === "preserve") return;
					if (action === "consume") {
						props.reviewDrafts.consumeDraft(
							draftToken,
							repoRoot,
							currentTarget,
							submittedDraft,
						);
						return;
					}
					props.reviewDrafts.clearDraft(draftToken, repoRoot, currentTarget);
				},
			}),
		);
		return "queued";
	}

	function closeReview(): void {
		props.onClose();
	}

	function closeReviewTab(): void {
		if (queueCurrentDraft({ showBlockedWarning: true }) === "blocked") return;
		(props.onTabClose ?? props.onClose)();
	}

	createEffect(() => {
		const register = props.onCloseRequestReady;
		if (!register) return;
		register(closeReviewTab);
		onCleanup(() => register(null));
	});

	async function submitReview(): Promise<void> {
		if (
			queueCurrentDraft({
				showEmptyWarning: true,
				showBlockedWarning: true,
			}) !== "queued"
		) {
			return;
		}
		const currentTarget = target();
		const otherDrafts = props.reviewDrafts.countDraftsExcept(
			draftToken,
			repoRoot,
			currentTarget,
		);
		if (otherDrafts > 0) {
			props.toast({
				title: "Other review drafts kept",
				subtitle: `${otherDrafts} draft note${otherDrafts === 1 ? "" : "s"} on other review targets ${otherDrafts === 1 ? "was" : "were"} not included.`,
				variant: "info",
			});
		}
		const submission = props.onSubmitMessage();
		setSubmittingReview(true);
		try {
			await submission;
		} finally {
			setSubmittingReview(false);
		}
	}

	// Saved review notes are a live projection into the composer. The explicit
	// submit binding only adds immediacy: it refreshes that projection and sends
	// the composer's current message.
	createEffect(() => {
		queueCurrentDraft();
	});

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
		props.onFocusRequest?.();

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

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => active() && !editorOpen() && !commitPickerOpen(),
		diagnosticsWhen: () => active() && !editorOpen() && !commitPickerOpen(),
		commands: {
			"review.open-file": openCurrentFileInWorkspace,
			"review.search-tree": props.onFindFile,
			"review.cycle-target": cycleTarget,
			"review.pick-commit": openCommitPicker,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => active() && editorOpen(),
		diagnosticsWhen: () => active() && editorOpen(),
		commands: {
			"review.close-editor": () => {
				if (editingRange()) closeRangeNoteEditor();
				else if (editingFileNoteKey()) closeFileNoteEditor();
			},
		},
	}));

	// Patch mode — diff view (changed files)
	useKeymapLayer(() => ({
		scope: "modal",
		when: () =>
			active() && !editorOpen() && !commitPickerOpen() && mode() === "patch",
		diagnosticsWhen: () =>
			active() && mode() === "patch" && !commitPickerOpen(),
		commands: {
			"review.back": () => {
				if (rangeAnchor()) setRangeAnchor(null);
				else showChangesTree();
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
			"review.submit": () => void submitReview(),
		},
	}));

	// Tree mode: view-level bindings (navigation is handled by FileTreePanel)
	useKeymapLayer(() => ({
		scope: "modal",
		when: () =>
			active() && !editorOpen() && mode() === "tree" && !commitPickerOpen(),
		diagnosticsWhen: () => active() && mode() === "tree" && !commitPickerOpen(),
		commands: {
			"review.file-note": () => {
				const file = selectedFile();
				if (file) openFileNoteEditor(file);
			},
			"review.toggle-view": () => toggleDiffView(),
			"review.clear-file-note": () => clearSelectedFileNote(),
			"review.submit": () => void submitReview(),
		},
	}));

	const reviewHeaderLeft = (
		<text fg={theme.textMuted}>
			<Show
				when={targetCommit()}
				fallback={<span style={{ fg: theme.textPrimary }}>working tree</span>}
			>
				{(commit) => (
					<>
						<span style={{ fg: theme.metaText }}>{commit().shortSha}</span>{" "}
						<span style={{ fg: theme.textPrimary }}>{commit().subject}</span>
					</>
				)}
			</Show>
		</text>
	);
	const reviewHeaderRight = (
		<text fg={theme.textMuted}>
			{targetCommit() ? `committed ${targetCommit()?.relativeTime} · ` : ""}
			{formatFileCount(reviewFiles().length)}
			{totalDraftNotes() > 0 ? ` · ${formatNoteCount(totalDraftNotes())}` : ""}
		</text>
	);
	const reviewHeader = (
		<WorkspacePanelHeader
			leading={
				<WorkspaceSidebarToggle
					expanded={paneVisibility().tree}
					onToggle={toggleChangesTree}
				/>
			}
			left={reviewHeaderLeft}
			right={reviewHeaderRight}
		/>
	);

	return (
		<WorkspacePanelLayout
			header={reviewHeader}
			footer={
				<KeymapHintBar
					borderless
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
				when={!filesLoading()}
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
					{/* Changed-files tree */}
					<Show when={paneVisibility().tree}>
						<box
							width={isWide() ? treePanelWidth() : undefined}
							flexGrow={isWide() ? 0 : 1}
							flexShrink={0}
							height="100%"
							border={isWide() ? ["right"] : []}
							borderColor={theme.borderDefault}
						>
							<FileTreePanel
								reviewFiles={reviewFiles()}
								focused={active() && mode() === "tree"}
								editorOpen={editorOpen()}
								finderOpen={commitPickerOpen()}
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
								onClose={closeReview}
								onFocusRequest={props.onFocusRequest}
							/>
						</box>
					</Show>

					{/* Diff pane */}
					<Show when={paneVisibility().diff}>
						<Show
							when={selectedFile()}
							fallback={
								<box flexGrow={1} justifyContent="center" alignItems="center">
									<text fg={theme.textMuted}>
										{reviewFiles().length === 0
											? "No changed files"
											: "Select a change to view"}
									</text>
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
											<box flexDirection="column" alignItems="flex-end">
												<Show when={staleReviewFileIds().has(file().id)}>
													<text fg={theme.metaText}>no longer changed</text>
												</Show>
												<Show when={sourceLabel(file())}>
													{(label) => (
														<text fg={theme.textMuted}>{label()}</text>
													)}
												</Show>
											</box>
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
												<box flexDirection="column" width="100%">
													{renderFileDiffContent(file(), mode() === "patch")}
												</box>
											</scrollbox>
										</box>
									</box>
								);
							}}
						</Show>
					</Show>
				</box>
				<Show when={commitPickerOpen()}>
					<CommitPickerDialog
						options={commitPickerOptions()}
						availableWidth={contentWidth()}
						onClose={() => {
							setCommitPickerOpen(false);
							setPickingBranchBase(false);
						}}
					/>
				</Show>
			</Show>
		</WorkspacePanelLayout>
	);
}

// ── Commit picker dialog ────────────────────────────────────

type CommitPickerDialogProps = {
	options: PickerOption[];
	availableWidth: number;
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
				minWidth={Math.min(72, Math.max(1, props.availableWidth - 2))}
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
