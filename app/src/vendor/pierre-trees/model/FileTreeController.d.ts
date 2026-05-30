/**
 * Declaration for vendored FileTreeController.
 * Only the methods relevant to Kit's file explorer are declared.
 */
import type {
	FileTreeBatchOperation,
	FileTreeControllerListener,
	FileTreeControllerOptions,
	FileTreeItemHandle,
	FileTreeMoveOptions,
	FileTreeMutationEventForType,
	FileTreeMutationEventType,
	FileTreePublicId,
	FileTreeRemoveOptions,
	FileTreeResetOptions,
	FileTreeScrollToPathOptions,
	FileTreeVisibleRow,
} from "../types.js";

export declare class FileTreeController {
	constructor(options: FileTreeControllerOptions);
	destroy(): void;

	// ── Focus ─────────────────────────────────────────────────────
	focusFirstItem(): void;
	focusLastItem(): void;
	focusNextItem(): void;
	focusPreviousItem(): void;
	focusParentItem(): void;
	focusPath(path: string): void;
	focusNearestPath(path: string | null): string | null;
	getFocusedItem(): FileTreeItemHandle | null;
	getFocusedPath(): string | null;
	getFocusedIndex(): number;

	// ── Selection ─────────────────────────────────────────────────
	getSelectedPaths(): readonly string[];
	selectOnlyPath(path: string): void;
	selectPath(path: string): void;
	deselectPath(path: string): void;
	toggleFocusedSelection(): void;
	togglePathSelection(path: string): void;
	selectPathRange(path: string, unionSelection: boolean): void;
	selectAllVisiblePaths(): void;
	extendSelectionFromFocused(offset: -1 | 1): void;

	// ── Visible rows ──────────────────────────────────────────────
	getVisibleCount(): number;
	getVisibleRows(start: number, end: number): readonly FileTreeVisibleRow[];
	getItem(path: string): FileTreeItemHandle | null;

	// ── Scroll ────────────────────────────────────────────────────
	scrollToPath(
		path: FileTreePublicId,
		options?: FileTreeScrollToPathOptions,
	): void;

	// ── Search ────────────────────────────────────────────────────
	setSearch(value: string | null): void;
	openSearch(initialValue?: string): void;
	closeSearch(): void;
	isSearchOpen(): boolean;
	getSearchValue(): string;
	getSearchMatchingPaths(): readonly string[];
	focusNextSearchMatch(): void;
	focusPreviousSearchMatch(): void;

	// ── Mutation ──────────────────────────────────────────────────
	add(path: string): void;
	remove(path: string, options?: FileTreeRemoveOptions): void;
	move(
		fromPath: string,
		toPath: string,
		options?: FileTreeMoveOptions,
	): void;
	batch(operations: readonly FileTreeBatchOperation[]): void;
	resetPaths(
		paths: readonly string[],
		options?: FileTreeResetOptions,
	): void;
	onMutation<TType extends FileTreeMutationEventType | "*">(
		type: TType,
		handler: (event: FileTreeMutationEventForType<TType>) => void,
	): () => void;

	// ── Subscribe ─────────────────────────────────────────────────
	subscribe(listener: FileTreeControllerListener): () => void;
}
