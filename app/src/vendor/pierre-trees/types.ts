/**
 * Type declarations for the vendored @pierre/trees model layer.
 *
 * Vendored from @pierre/trees@1.0.0-beta.4
 * Only the types needed for Kit's file explorer are declared here.
 */

// ── Path identifiers ────────────────────────────────────────────────

/** Public tree identity — always a path string. */
export type FileTreePublicId = string;

// ── Prepared input ──────────────────────────────────────────────────

/** Opaque pre-processed path set for efficient tree construction. */
export interface FileTreePreparedInput {
	readonly paths: readonly string[];
}

// ── Sort ────────────────────────────────────────────────────────────

export interface FileTreeSortEntry {
	basename: string;
	depth: number;
	isDirectory: boolean;
	path: FileTreePublicId;
	segments: readonly string[];
}

export type FileTreeSortComparator = (
	left: FileTreeSortEntry,
	right: FileTreeSortEntry,
) => number;

// ── Expansion ───────────────────────────────────────────────────────

/** `'closed'` | `'open'` | depth number for initial expansion. */
export type FileTreeInitialExpansion = "closed" | "open" | number;

// ── Search ──────────────────────────────────────────────────────────

export type FileTreeSearchMode =
	| "expand-matches"
	| "collapse-non-matches"
	| "hide-non-matches";

export type FileTreeSearchChangeListener = (value: string | null) => void;

// ── Visible rows ────────────────────────────────────────────────────

export interface FileTreeVisibleSegment {
	isTerminal: boolean;
	name: string;
	path: FileTreePublicId;
}

export interface FileTreeVisibleRow {
	ancestorPaths: readonly FileTreePublicId[];
	depth: number;
	flattenedSegments?: readonly FileTreeVisibleSegment[];
	hasChildren: boolean;
	index: number;
	isFocused: boolean;
	isSelected: boolean;
	isExpanded: boolean;
	isFlattened: boolean;
	kind: "directory" | "file";
	level: number;
	name: string;
	path: FileTreePublicId;
	posInSet: number;
	setSize: number;
}

// ── Item handles ────────────────────────────────────────────────────

export interface FileTreeItemHandleBase {
	deselect(): void;
	focus(): void;
	getPath(): FileTreePublicId;
	isFocused(): boolean;
	isDirectory(): boolean;
	isSelected(): boolean;
	select(): void;
	toggleSelect(): void;
}

export interface FileTreeDirectoryHandle extends FileTreeItemHandleBase {
	collapse(): void;
	expand(): void;
	isDirectory(): true;
	isExpanded(): boolean;
	toggle(): void;
}

export interface FileTreeFileHandle extends FileTreeItemHandleBase {
	isDirectory(): false;
}

export type FileTreeItemHandle =
	| FileTreeDirectoryHandle
	| FileTreeFileHandle;

// ── Scroll ──────────────────────────────────────────────────────────

export type FileTreeScrollOffset = "top" | "center" | "nearest";

export interface FileTreeScrollToPathOptions {
	focus?: boolean;
	offset?: FileTreeScrollOffset;
}

// ── Selection ───────────────────────────────────────────────────────

export type FileTreeSelectionChangeListener = (
	selectedPaths: readonly FileTreePublicId[],
) => void;

// ── Mutation ────────────────────────────────────────────────────────

export interface FileTreeRemoveOptions {
	recursive?: boolean;
}

export type FileTreeCollisionStrategy = "error" | "replace" | "skip";

export interface FileTreeMoveOptions {
	collision?: FileTreeCollisionStrategy;
}

export type FileTreeBatchOperation =
	| { path: FileTreePublicId; type: "add" }
	| ({ path: FileTreePublicId; type: "remove" } & FileTreeRemoveOptions)
	| ({
			from: FileTreePublicId;
			to: FileTreePublicId;
			type: "move";
		} & FileTreeMoveOptions);

export interface FileTreeMutationEventInvalidation {
	canonicalChanged: boolean;
	projectionChanged: boolean;
	visibleCountDelta: number | null;
}

export interface FileTreeAddEvent extends FileTreeMutationEventInvalidation {
	operation: "add";
	path: FileTreePublicId;
}

export interface FileTreeRemoveEvent extends FileTreeMutationEventInvalidation {
	operation: "remove";
	path: FileTreePublicId;
	recursive: boolean;
}

export interface FileTreeMoveEvent extends FileTreeMutationEventInvalidation {
	from: FileTreePublicId;
	operation: "move";
	to: FileTreePublicId;
}

export interface FileTreeResetEvent extends FileTreeMutationEventInvalidation {
	operation: "reset";
	pathCountAfter: number;
	pathCountBefore: number;
	usedPreparedInput: boolean;
}

export type FileTreeMutationSemanticEvent =
	| FileTreeAddEvent
	| FileTreeRemoveEvent
	| FileTreeMoveEvent
	| FileTreeResetEvent;

export interface FileTreeBatchEvent
	extends FileTreeMutationEventInvalidation {
	events: readonly FileTreeMutationSemanticEvent[];
	operation: "batch";
}

export type FileTreeMutationEvent =
	| FileTreeMutationSemanticEvent
	| FileTreeBatchEvent;

export type FileTreeMutationEventType = FileTreeMutationEvent["operation"];

export type FileTreeMutationEventForType<
	TType extends FileTreeMutationEventType | "*",
> = TType extends "*"
	? FileTreeMutationEvent
	: Extract<FileTreeMutationEvent, { operation: TType }>;

export interface FileTreeResetOptions {
	initialExpandedPaths?: readonly FileTreePublicId[];
	preparedInput?: FileTreePreparedInput;
}

// ── Controller options ──────────────────────────────────────────────

export interface FileTreeControllerOptions {
	/** File paths to display in the tree. */
	paths?: readonly FileTreePublicId[];
	/** Pre-processed input (alternative to `paths`). */
	preparedInput?: FileTreePreparedInput;
	/** Collapse single-child directory chains (e.g. `src/utils/` → `src/utils/`). */
	flattenEmptyDirectories?: boolean;
	/** How deep directories are expanded on init. Default: `'closed'`. */
	initialExpansion?: FileTreeInitialExpansion;
	/** Paths to expand on init (overrides `initialExpansion` for listed paths). */
	initialExpandedPaths?: readonly FileTreePublicId[];
	/** Sort order. Default: directories-first alphabetical. */
	sort?: "default" | FileTreeSortComparator;
	/** Whether paths are already sorted. Skips internal sort for performance. */
	presorted?: boolean;
	/** Search filter mode. */
	fileTreeSearchMode?: FileTreeSearchMode;
	/** Initial search query. */
	initialSearchQuery?: string | null;
	/** Paths to select on init. */
	initialSelectedPaths?: readonly FileTreePublicId[];
	/** Search change callback. */
	onSearchChange?: FileTreeSearchChangeListener;
}

// ── Controller listener ─────────────────────────────────────────────

export type FileTreeControllerListener = () => void;
