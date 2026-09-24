/**
 * Vendored model layer from @pierre/trees@1.0.0-beta.4
 *
 * Provides a headless file tree controller: path-based tree construction,
 * expand/collapse, focus/selection, search, and mutation — without any
 * rendering or DOM dependencies.
 *
 * Only the model + path-store layers are vendored. The rendering layer
 * (Preact web component) is not included.
 */

export { FileTreeController } from "./model/FileTreeController.js";
export {
	prepareFileTreeInput,
	preparePresortedFileTreeInput,
} from "./preparedInput.js";

export type {
	FileTreeBatchOperation,
	FileTreeControllerListener,
	FileTreeControllerOptions,
	FileTreeDirectoryHandle,
	FileTreeFileHandle,
	FileTreeInitialExpansion,
	FileTreeItemHandle,
	FileTreeItemHandleBase,
	FileTreeMoveOptions,
	FileTreeMutationEvent,
	FileTreeMutationEventForType,
	FileTreeMutationEventType,
	FileTreePreparedInput,
	FileTreePublicId,
	FileTreeRemoveOptions,
	FileTreeResetOptions,
	FileTreeScrollOffset,
	FileTreeScrollToPathOptions,
	FileTreeSearchMode,
	FileTreeSelectionChangeListener,
	FileTreeSortComparator,
	FileTreeSortEntry,
	FileTreeVisibleRow,
	FileTreeVisibleSegment,
} from "./types.js";
