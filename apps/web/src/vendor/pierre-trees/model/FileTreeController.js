import { PathStore } from "../path-store/src/store.js";
import "../path-store/src/index.js";
import { renameFileTreePaths } from "../utils/renameFileTreePaths.js";
import { buildDropOperations, createDropContext, dropTargetsEqual, isSelfOrDescendantDrop, resolveDraggedPathsForStart } from "./dragAndDrop.js";
import { resolveFileTreeInput } from "./inputResolution.js";
import { isPathMutationEvent, remapPathThroughMutation, toTreesMutationEvent } from "./mutationEvents.js";
import { arePathSetsEqual, getAncestorDirectoryPaths, getImmediateParentPath, getSiblingComparisonKey, isCanonicalDirectoryPath, toLowerCaseSearchPath } from "./pathHelpers.js";
import { getRenameLeafName, toCanonicalRenamePath, toRenameHelperPath } from "./renameHelpers.js";
import { normalizeSearchQuery } from "./searchHelpers.js";

//#region src/model/FileTreeController.ts
const FILE_TREE_RENAME_VIEW = Symbol("FILE_TREE_RENAME_VIEW");
const INITIAL_PROJECTION_ROW_LIMIT = 512;
const CONTEXT_VISIBLE_ROW_RANGE_LIMIT = 512;
function normalizeScrollOffset(offset) {
	return offset === "top" || offset === "center" ? offset : "nearest";
}
function resolveFocusedIndexByLookup(rowCount, getVisibleIndex, candidatePath) {
	if (rowCount === 0) return -1;
	if (candidatePath != null) {
		const directIndex = getVisibleIndex(candidatePath);
		if (directIndex != null) return directIndex;
		const ancestorPaths = getAncestorDirectoryPaths(candidatePath);
		for (let index = ancestorPaths.length - 1; index >= 0; index -= 1) {
			const ancestorPath = ancestorPaths[index];
			if (ancestorPath == null) continue;
			const ancestorIndex = getVisibleIndex(ancestorPath);
			if (ancestorIndex != null) return ancestorIndex;
		}
	}
	return 0;
}
function createVisibleProjection(projection, focusedPathCandidate, resolveVisibleIndexByPath) {
	if (projection.paths.length === 0) return {
		focusedIndex: -1,
		getParentIndex: projection.getParentIndex,
		paths: projection.paths,
		posInSetByIndex: projection.posInSetByIndex,
		setSizeByIndex: projection.setSizeByIndex
	};
	if (focusedPathCandidate == null) return {
		focusedIndex: 0,
		getParentIndex: projection.getParentIndex,
		paths: projection.paths,
		posInSetByIndex: projection.posInSetByIndex,
		setSizeByIndex: projection.setSizeByIndex
	};
	const getVisibleIndex = resolveVisibleIndexByPath ?? ((path) => projection.visibleIndexByPath.get(path) ?? null);
	return {
		focusedIndex: resolveFocusedIndexByLookup(projection.paths.length, getVisibleIndex, focusedPathCandidate),
		getParentIndex: projection.getParentIndex,
		paths: projection.paths,
		posInSetByIndex: projection.posInSetByIndex,
		setSizeByIndex: projection.setSizeByIndex
	};
}
/**
* Owns the live PathStore instance and exposes a path-first boundary without
* leaking internal store IDs.
*/
var FileTreeController = class {
	#baseOptions;
	#listeners = /* @__PURE__ */ new Set();
	#mutationListeners = /* @__PURE__ */ new Map();
	#dragAndDropConfig = null;
	#dragSession = null;
	#ancestorIndicesByIndex = /* @__PURE__ */ new Map();
	#ancestorPathsByIndex = /* @__PURE__ */ new Map();
	#focusedIndex = -1;
	#focusedPath = null;
	#hasFullProjection = false;
	#getParentIndexForVisibleRow = (_index) => -1;
	#itemHandles = /* @__PURE__ */ new Map();
	#knownDirectoryPaths = null;
	#knownDirectoryPathsLowerCase = null;
	#knownPaths = null;
	#listedPaths = null;
	#listedPathsLowerCase = null;
	#onRename;
	#onRenameError;
	#onSearchChange;
	#projectionPaths = [];
	#projectionPosInSetByIndex = new Int32Array(0);
	#projectionSetSizeByIndex = new Int32Array(0);
	#renameCanRename = void 0;
	#renameEnabled = false;
	#renamingPath = null;
	#renamingValue = "";
	#removeRenamingPathIfCanceled = false;
	#searchMatchPathSet = /* @__PURE__ */ new Set();
	#searchMatchingPaths = [];
	#searchMode;
	#searchPreviousExpandedPaths = null;
	#searchValue = null;
	#searchVisiblePathSet = null;
	#searchVisibleIndexByPath = null;
	#searchVisibleIndices = null;
	#searchVisiblePaths = null;
	#scrollRequest = null;
	#scrollRequestId = 0;
	#selectionAnchorPath = null;
	#selectedPaths = /* @__PURE__ */ new Set();
	#selectionVersion = 0;
	#store;
	#storeVisibleCount = 0;
	#suppressStoreNotifications = false;
	#visibleCount = 0;
	#unsubscribe;
	constructor(options) {
		const { dragAndDrop, fileTreeSearchMode, initialSearchQuery, initialSelectedPaths, renaming, onSearchChange, paths, preparedInput,...baseOptions } = options;
		const resolvedInput = resolveFileTreeInput({
			paths,
			preparedInput
		}, "constructor", baseOptions.sort);
		this.#baseOptions = baseOptions;
		if (dragAndDrop != null && dragAndDrop !== false) this.#dragAndDropConfig = dragAndDrop === true ? {} : dragAndDrop;
		this.#renameEnabled = renaming != null && renaming !== false;
		if (renaming != null && renaming !== false && renaming !== true) {
			this.#renameCanRename = renaming.canRename;
			this.#onRenameError = renaming.onError;
			this.#onRename = renaming.onRename;
		}
		this.#onSearchChange = onSearchChange;
		this.#searchMode = fileTreeSearchMode ?? "hide-non-matches";
		this.#store = this.#createStore(resolvedInput.paths, resolvedInput.preparedInput);
		const resolvedInitialSelectedPaths = initialSelectedPaths?.map((path) => this.#resolveSelectionPath(path)).filter((resolved) => resolved != null) ?? [];
		const initialFocusedPath = resolvedInitialSelectedPaths.at(-1) ?? null;
		if (resolvedInitialSelectedPaths.length > 0) {
			this.#selectedPaths = new Set(resolvedInitialSelectedPaths);
			this.#selectionAnchorPath = initialFocusedPath;
			this.#selectionVersion = 1;
		}
		this.#rebuildVisibleProjection(initialFocusedPath, false);
		if (initialSearchQuery != null) this.#setSearchState(initialSearchQuery, false);
		this.#unsubscribe = this.#subscribe();
	}
	destroy() {
		this.#unsubscribe?.();
		this.#unsubscribe = null;
		this.#mutationListeners.clear();
		this.#listeners.clear();
		this.#itemHandles.clear();
		this.#dragSession = null;
		this.#invalidateKnownPathCaches();
	}
	focusFirstItem() {
		if (this.#getCurrentVisiblePaths().length > 0) this.#setFocusedIndex(0);
	}
	focusLastItem() {
		if (this.#visibleCount <= 0) return;
		this.#ensureFullProjection();
		this.#setFocusedIndex(this.#visibleCount - 1);
	}
	focusNextItem() {
		this.#moveFocus(1);
	}
	focusParentItem() {
		if (this.#focusedPath == null) return;
		const parentPath = getImmediateParentPath(this.#focusedPath);
		if (parentPath == null) return;
		const nextFocusedIndex = this.#resolveFocusedIndex(parentPath);
		if (nextFocusedIndex >= 0) this.#setFocusedIndex(nextFocusedIndex);
	}
	focusPath(path) {
		const resolvedPath = this.#store.getPathInfo(path)?.path ?? null;
		if (resolvedPath == null) return;
		this.#ensureFullProjection();
		const nextFocusedIndex = this.#resolveFocusedIndex(resolvedPath);
		if (nextFocusedIndex >= 0) this.#setFocusedIndex(nextFocusedIndex);
	}
	scrollToPath(path, options) {
		const resolvedPath = this.#store.getPathInfo(path)?.path ?? null;
		if (resolvedPath == null) return;
		this.#ensureFullProjection();
		const targetIndex = this.#getExactCurrentVisibleIndexByPath(resolvedPath);
		if (targetIndex < 0) return;
		if (this.#resolveVisiblePathAtIndex(targetIndex) == null) return;
		if (options?.focus !== false) this.#setFocusedIndex(targetIndex, false);
		this.#scrollRequest = {
			id: this.#scrollRequestId += 1,
			offset: normalizeScrollOffset(options?.offset),
			visibleIndex: targetIndex
		};
		this.#emit();
	}
	focusMountedPathFromInput(path) {
		const resolvedPath = this.#store.getPathInfo(path)?.path ?? null;
		if (resolvedPath == null) return;
		const nextFocusedIndex = this.#resolveFocusedIndex(resolvedPath);
		if (nextFocusedIndex >= 0) this.#setFocusedIndex(nextFocusedIndex);
	}
	focusNearestPath(path) {
		const nextPath = this.resolveNearestVisiblePath(path);
		if (nextPath == null) return null;
		const nextFocusedIndex = this.#resolveFocusedIndex(nextPath);
		if (nextFocusedIndex >= 0) {
			this.#setFocusedIndex(nextFocusedIndex);
			return this.#getCurrentVisiblePaths()[nextFocusedIndex] ?? nextPath;
		}
		return null;
	}
	focusPreviousItem() {
		this.#moveFocus(-1);
	}
	getFocusedIndex() {
		return this.#focusedIndex;
	}
	getFocusedItem() {
		return this.#focusedPath == null ? null : this.#getOrCreateItemHandle(this.#focusedPath);
	}
	getFocusedPath() {
		return this.#focusedPath;
	}
	getScrollRequest() {
		return this.#scrollRequest;
	}
	clearScrollRequest(id) {
		if (this.#scrollRequest?.id === id) this.#scrollRequest = null;
	}
	resolveNearestVisiblePath(path) {
		const currentVisiblePaths = this.#getCurrentVisiblePaths();
		if (this.#visibleCount === 0) return null;
		if (path == null) return this.#focusedPath ?? currentVisiblePaths[0] ?? null;
		const resolvedPath = this.#store.getPathInfo(path)?.path ?? path;
		const directIndex = this.#resolveFocusedIndex(resolvedPath);
		if (directIndex >= 0) return currentVisiblePaths[directIndex] ?? resolvedPath;
		const siblingPath = this.#findNearestVisibleSiblingPath(resolvedPath);
		if (siblingPath != null) return siblingPath;
		return this.#focusedPath ?? currentVisiblePaths[0] ?? null;
	}
	getSelectedPaths() {
		return [...this.#selectedPaths];
	}
	getSelectionVersion() {
		return this.#selectionVersion;
	}
	getVisibleCount() {
		return this.#visibleCount;
	}
	getVisibleRows(start, end) {
		if (end < start || this.#visibleCount === 0) return [];
		const boundedStart = Math.max(0, start);
		const boundedEnd = Math.min(this.#visibleCount - 1, end);
		if (boundedEnd < boundedStart) return [];
		const boundedLength = boundedEnd - boundedStart + 1;
		if (this.#searchVisibleIndices == null && !this.#hasFullProjection && boundedEnd >= this.#projectionPaths.length && boundedLength <= CONTEXT_VISIBLE_ROW_RANGE_LIMIT) {
			const rows = [];
			for (let index = boundedStart; index <= boundedEnd; index += 1) {
				const context = this.#store.getVisibleRowContext(index);
				if (context == null) break;
				rows.push(this.#createVisibleRowFromContext(context));
			}
			return rows;
		}
		if (!this.#hasFullProjection && boundedEnd >= this.#projectionPaths.length) this.#ensureFullProjection();
		if (this.#searchVisibleIndices != null) {
			const projectionIndices = Array.from({ length: boundedEnd - boundedStart + 1 }, (_, visibleOffset) => this.#getProjectionIndexFromVisibleIndex(boundedStart + visibleOffset));
			const visibleRowByProjectionIndex = /* @__PURE__ */ new Map();
			let runStartIndex = projectionIndices[0] ?? -1;
			let runEndIndex = runStartIndex;
			for (let index = 1; index <= projectionIndices.length; index += 1) {
				const projectionIndex = projectionIndices[index];
				if (projectionIndex != null && projectionIndex === runEndIndex + 1) {
					runEndIndex = projectionIndex;
					continue;
				}
				if (runStartIndex >= 0) this.#store.getVisibleSlice(runStartIndex, runEndIndex).forEach((row, offset) => {
					visibleRowByProjectionIndex.set(runStartIndex + offset, row);
				});
				if (projectionIndex == null) {
					runStartIndex = -1;
					runEndIndex = -1;
					continue;
				}
				runStartIndex = projectionIndex;
				runEndIndex = projectionIndex;
			}
			return Array.from({ length: boundedEnd - boundedStart + 1 }, (_, visibleOffset) => {
				const visibleIndex = boundedStart + visibleOffset;
				const projectionIndex = this.#getProjectionIndexFromVisibleIndex(visibleIndex);
				const row = visibleRowByProjectionIndex.get(projectionIndex);
				const projectionPath = this.#projectionPaths[projectionIndex];
				if (row == null || projectionPath == null) throw new Error(`Missing projection row for filtered visible index ${String(visibleIndex)}`);
				return this.#createVisibleRow(row, visibleIndex, projectionIndex, {
					ancestorPaths: this.#getAncestorPaths(projectionIndex),
					path: projectionPath
				});
			});
		}
		return this.#store.getVisibleSlice(boundedStart, boundedEnd).map((row, offset) => {
			const index = boundedStart + offset;
			const projectionPath = this.#projectionPaths[index];
			if (projectionPath == null) throw new Error(`Missing projection path for visible index ${String(index)}`);
			return this.#createVisibleRow(row, index, index, {
				ancestorPaths: this.#getAncestorPaths(index),
				path: projectionPath
			});
		});
	}
	getStickyRowCandidates(scrollTop, itemHeight) {
		if (this.#searchVisibleIndices != null) return null;
		if (this.#visibleCount === 0 || scrollTop <= 0 || itemHeight <= 0) return [];
		const stickyRows = [];
		for (let slotDepth = 0; slotDepth < this.#visibleCount; slotDepth += 1) {
			const slotTop = scrollTop + slotDepth * itemHeight;
			const thresholdIndex = Math.min(this.#visibleCount - 1, Math.floor(slotTop / itemHeight));
			const candidateContext = this.#getStickyCandidateContextAt(thresholdIndex, slotDepth) ?? (thresholdIndex > 0 ? this.#getStickyCandidateContextAt(thresholdIndex - 1, slotDepth) : void 0);
			if (candidateContext == null) break;
			stickyRows.push({
				row: this.#createVisibleRowFromContext(candidateContext),
				subtreeEndIndex: candidateContext.subtreeEndIndex
			});
		}
		return stickyRows;
	}
	/**
	* Returns the item handle for the given path.
	*
	* Accepts both canonical directory paths (`src/`) and bare directory lookup
	* paths (`src`) so callers do not need to know the canonical slash rules.
	*/
	getItem(path) {
		const itemInfo = this.#store.getPathInfo(path);
		return itemInfo == null ? null : this.#getOrCreateItemHandle(itemInfo.path, itemInfo);
	}
	resolveMountedDirectoryPathFromInput(path) {
		const pathInfo = this.#store.getPathInfo(path);
		return pathInfo?.kind === "directory" ? pathInfo.path : null;
	}
	toggleMountedDirectoryFromInput(path) {
		const directoryPath = this.resolveMountedDirectoryPathFromInput(path);
		if (directoryPath == null) return;
		this.#toggleDirectory(directoryPath);
	}
	selectAllVisiblePaths() {
		this.#ensureFullProjection();
		const nextSelectedPaths = [...this.#getCurrentVisiblePaths()];
		this.#applySelection(nextSelectedPaths, this.#focusedPath ?? this.#selectionAnchorPath);
	}
	selectOnlyPath(path) {
		const resolvedPath = this.#resolveSelectionPath(path);
		if (resolvedPath == null) return;
		this.#applySelection([resolvedPath], resolvedPath);
	}
	selectOnlyMountedPathFromInput(path) {
		this.#applySelection([path], path);
	}
	selectPath(path) {
		const resolvedPath = this.#resolveSelectionPath(path);
		if (resolvedPath == null || this.#selectedPaths.has(resolvedPath)) return;
		this.#applySelection([...this.#selectedPaths, resolvedPath]);
	}
	deselectPath(path) {
		const resolvedPath = this.#resolveSelectionPath(path);
		if (resolvedPath == null || !this.#selectedPaths.has(resolvedPath)) return;
		this.#applySelection([...this.#selectedPaths].filter((selectedPath) => selectedPath !== resolvedPath));
	}
	toggleFocusedSelection() {
		if (this.#focusedPath == null) return;
		this.togglePathSelectionFromInput(this.#focusedPath);
	}
	togglePathSelection(path) {
		const resolvedPath = this.#resolveSelectionPath(path);
		if (resolvedPath == null) return;
		if (this.#selectedPaths.has(resolvedPath)) {
			this.deselectPath(resolvedPath);
			return;
		}
		this.selectPath(resolvedPath);
	}
	togglePathSelectionFromInput(path) {
		const resolvedPath = this.#resolveSelectionPath(path);
		if (resolvedPath == null) return;
		if (this.#selectedPaths.has(resolvedPath)) {
			this.#applySelection([...this.#selectedPaths].filter((selectedPath) => selectedPath !== resolvedPath), resolvedPath);
			return;
		}
		this.#applySelection([...this.#selectedPaths, resolvedPath], resolvedPath);
	}
	selectPathRange(path, unionSelection) {
		const resolvedPath = this.#resolveSelectionPath(path);
		if (resolvedPath == null) return;
		this.#ensureFullProjection();
		const anchorPath = this.#selectionAnchorPath;
		const anchorIndex = anchorPath == null ? -1 : this.#getVisibleIndexByPath(anchorPath);
		const targetIndex = this.#getVisibleIndexByPath(resolvedPath);
		if (anchorIndex === -1 || targetIndex === -1) {
			const nextSelectedPaths$1 = unionSelection ? [...this.#selectedPaths, resolvedPath] : [resolvedPath];
			this.#applySelection(nextSelectedPaths$1, resolvedPath);
			return;
		}
		const [startIndex, endIndex] = anchorIndex <= targetIndex ? [anchorIndex, targetIndex] : [targetIndex, anchorIndex];
		const rangePaths = this.#getCurrentVisiblePaths().slice(startIndex, endIndex + 1);
		const nextSelectedPaths = unionSelection ? [...this.#selectedPaths, ...rangePaths] : rangePaths;
		this.#applySelection(nextSelectedPaths, anchorPath);
	}
	extendSelectionFromFocused(offset) {
		if (this.#focusedPath == null) return;
		const focusedIndex = this.#focusedIndex;
		if (focusedIndex === -1) return;
		const nextIndex = Math.min(this.#visibleCount - 1, Math.max(0, focusedIndex + offset));
		if (nextIndex === focusedIndex) return;
		if (!this.#hasFullProjection && nextIndex >= this.#projectionPaths.length) this.#ensureFullProjection();
		const visiblePaths = this.#getCurrentVisiblePaths();
		const currentPath = visiblePaths[focusedIndex] ?? null;
		const nextPath = visiblePaths[nextIndex] ?? null;
		if (currentPath == null || nextPath == null) return;
		const nextSelectedPaths = new Set(this.#selectedPaths);
		if (nextSelectedPaths.has(currentPath) && nextSelectedPaths.has(nextPath)) nextSelectedPaths.delete(currentPath);
		else nextSelectedPaths.add(nextPath);
		this.#applySelection([...nextSelectedPaths], this.#selectionAnchorPath ?? currentPath, false);
		this.#setFocusedIndex(nextIndex);
	}
	getDragAndDropConfig() {
		return this.#dragAndDropConfig;
	}
	isDragAndDropEnabled() {
		return this.#dragAndDropConfig != null;
	}
	getDragSession() {
		if (this.#dragSession == null) return null;
		return {
			draggedPaths: [...this.#dragSession.draggedPaths],
			primaryPath: this.#dragSession.primaryPath,
			target: this.#dragSession.target == null ? null : { ...this.#dragSession.target }
		};
	}
	startDrag(path) {
		if (this.#dragAndDropConfig == null) return false;
		const resolvedPath = this.#resolveSelectionPath(path);
		if (resolvedPath == null) return false;
		if (this.#searchValue != null && this.#searchValue.length > 0) return false;
		const selectedPaths = this.getSelectedPaths();
		const draggedPaths = resolveDraggedPathsForStart(resolvedPath, selectedPaths);
		if (this.#dragAndDropConfig.canDrag?.(draggedPaths) === false) return false;
		if (!selectedPaths.includes(resolvedPath)) this.#applySelection([resolvedPath], resolvedPath, false);
		this.#focusPathWithoutEmit(resolvedPath);
		this.#dragSession = {
			draggedPaths,
			primaryPath: resolvedPath,
			target: null
		};
		this.#emit();
		return true;
	}
	setDragTarget(target) {
		const dragSession = this.#dragSession;
		if (dragSession == null) return;
		let nextTarget = target;
		if (nextTarget != null) {
			const context = createDropContext(dragSession.draggedPaths, nextTarget);
			if (isSelfOrDescendantDrop(dragSession.draggedPaths, nextTarget) || this.#dragAndDropConfig?.canDrop?.(context) === false) nextTarget = null;
		}
		if (dropTargetsEqual(dragSession.target, nextTarget)) return;
		this.#dragSession = {
			...dragSession,
			target: nextTarget
		};
		this.#emit();
	}
	cancelDrag() {
		if (this.#dragSession == null) return;
		this.#dragSession = null;
		this.#emit();
	}
	completeDrag() {
		const dragSession = this.#dragSession;
		if (dragSession == null) return false;
		this.#dragSession = null;
		const target = dragSession.target == null ? null : { ...dragSession.target };
		if (target == null) {
			this.#emit();
			return false;
		}
		const dropContext = createDropContext(dragSession.draggedPaths, target);
		if (isSelfOrDescendantDrop(dragSession.draggedPaths, target) || this.#dragAndDropConfig?.canDrop?.(dropContext) === false) {
			this.#emit();
			return false;
		}
		const dropPlan = buildDropOperations(dragSession.draggedPaths, target);
		if (dropPlan == null) {
			this.#emit();
			return false;
		}
		try {
			if (dropPlan.operations.length === 1) {
				const singleOperation = dropPlan.operations[0];
				if (singleOperation == null || singleOperation.type !== "move") throw new Error("Expected a single move operation for one-item drops");
				this.#store.move(singleOperation.from, singleOperation.to, { collision: singleOperation.collision });
			} else {
				this.#validateBatchDropOperations(dropPlan.operations);
				this.#store.batch(dropPlan.operations);
			}
		} catch (error) {
			this.#emit();
			this.#dragAndDropConfig?.onDropError?.(error instanceof Error ? error.message : String(error), dropContext);
			return false;
		}
		this.#dragAndDropConfig?.onDropComplete?.(dropPlan.result);
		return true;
	}
	subscribe(listener) {
		this.#listeners.add(listener);
		listener();
		return () => {
			this.#listeners.delete(listener);
		};
	}
	/**
	* Applies one file/directory addition through the shared mutation handle
	* without exposing the raw store to tree consumers.
	*/
	add(path) {
		this.#store.add(path);
	}
	remove(path, options = {}) {
		this.#store.remove(path, options);
	}
	move(fromPath, toPath, options = {}) {
		this.#store.move(fromPath, toPath, options);
	}
	batch(operations) {
		this.#store.batch(operations);
	}
	onMutation(type, handler) {
		const key = type;
		const typedHandler = handler;
		let listenersForType = this.#mutationListeners.get(key);
		if (listenersForType == null) {
			listenersForType = /* @__PURE__ */ new Set();
			this.#mutationListeners.set(key, listenersForType);
		}
		listenersForType.add(typedHandler);
		return () => {
			const registeredListeners = this.#mutationListeners.get(key);
			registeredListeners?.delete(typedHandler);
			if (registeredListeners?.size === 0) this.#mutationListeners.delete(key);
		};
	}
	setSearch(value) {
		this.#setSearchState(value, true);
	}
	openSearch(initialValue = "") {
		this.#setSearchState(initialValue, true);
	}
	closeSearch() {
		this.#setSearchState(null, true);
	}
	isSearchOpen() {
		return this.#searchValue !== null;
	}
	getSearchValue() {
		return this.#searchValue ?? "";
	}
	getSearchMatchingPaths() {
		return this.#searchMatchingPaths;
	}
	focusNextSearchMatch() {
		this.#focusRelativeSearchMatch(1);
	}
	focusPreviousSearchMatch() {
		this.#focusRelativeSearchMatch(-1);
	}
	startRenaming(path = this.#focusedPath ?? "", options = {}) {
		if (!this.#renameEnabled) return false;
		const itemInfo = this.#store.getPathInfo(path);
		if (itemInfo == null) return false;
		const canonicalPath = itemInfo.path;
		const isFolder = isCanonicalDirectoryPath(canonicalPath);
		const publicPath = toRenameHelperPath(canonicalPath);
		if (this.#renameCanRename?.({
			isFolder,
			path: publicPath
		}) === false) return false;
		for (const ancestorPath of getAncestorDirectoryPaths(canonicalPath)) if (!this.#store.isExpanded(ancestorPath)) this.#store.expand(ancestorPath);
		this.#applySelection([canonicalPath], canonicalPath, false);
		if (this.#searchValue != null) {
			this.#setSearchState(null, false);
			this.#onSearchChange?.(this.#searchValue);
		}
		this.#focusPathWithoutEmit(canonicalPath);
		this.#renamingPath = canonicalPath;
		this.#renamingValue = getRenameLeafName(canonicalPath);
		this.#removeRenamingPathIfCanceled = options.removeIfCanceled ?? false;
		this.#emit();
		return true;
	}
	[FILE_TREE_RENAME_VIEW]() {
		return {
			cancel: () => {
				this.#cancelRenaming();
			},
			commit: () => {
				this.#completeRenaming();
			},
			getPath: () => this.#renamingPath,
			getValue: () => this.#renamingValue,
			isActive: () => this.#renamingPath != null,
			setValue: (value) => {
				this.#setRenamingValue(value);
			}
		};
	}
	#cancelRenaming() {
		if (this.#renamingPath == null) return;
		const renamingPath = this.#renamingPath;
		const removePlaceholderEntry = this.#removeRenamingPathIfCanceled;
		this.#renamingPath = null;
		this.#renamingValue = "";
		this.#removeRenamingPathIfCanceled = false;
		if (removePlaceholderEntry) {
			this.remove(renamingPath, isCanonicalDirectoryPath(renamingPath) ? { recursive: true } : void 0);
			return;
		}
		this.#focusPathWithoutEmit(renamingPath);
		this.#emit();
	}
	#completeRenaming() {
		const renamingPath = this.#renamingPath;
		if (renamingPath == null) return;
		if (this.#removeRenamingPathIfCanceled && this.#renamingValue.trim().length === 0) {
			this.#renamingPath = null;
			this.#renamingValue = "";
			this.#removeRenamingPathIfCanceled = false;
			this.remove(renamingPath, isCanonicalDirectoryPath(renamingPath) ? { recursive: true } : void 0);
			return;
		}
		const isFolder = isCanonicalDirectoryPath(renamingPath);
		const result = renameFileTreePaths({
			files: this.#store.list(),
			isFolder,
			nextBasename: this.#renamingValue,
			path: toRenameHelperPath(renamingPath)
		});
		this.#renamingPath = null;
		this.#renamingValue = "";
		this.#removeRenamingPathIfCanceled = false;
		if ("error" in result) {
			this.#focusPathWithoutEmit(renamingPath);
			this.#onRenameError?.(result.error);
			this.#emit();
			return;
		}
		if (result.sourcePath === result.destinationPath) {
			this.#focusPathWithoutEmit(renamingPath);
			this.#emit();
			return;
		}
		this.#onRename?.({
			destinationPath: result.destinationPath,
			isFolder: result.isFolder,
			sourcePath: result.sourcePath
		});
		this.move(toCanonicalRenamePath(result.sourcePath, isFolder), toCanonicalRenamePath(result.destinationPath, isFolder));
	}
	#setRenamingValue(value) {
		if (this.#renamingPath == null || this.#renamingValue === value) return;
		this.#renamingValue = value;
		this.#emit();
	}
	/**
	* Rebuilds the controller around a new full path set. This is intentionally a
	* coarse whole-tree reset path rather than a localized mutation fast path.
	*/
	resetPaths(paths, options = {}) {
		const previousPathCount = this.#store.list().length;
		const previousVisibleCount = this.#visibleCount;
		const resolvedInput = resolveFileTreeInput({
			paths,
			preparedInput: options.preparedInput
		}, "resetPaths", this.#baseOptions.sort);
		const nextStore = this.#createStore(resolvedInput.paths, resolvedInput.preparedInput, options.initialExpandedPaths);
		const previousFocusedPath = this.#focusedPath;
		const previousRenamingPath = this.#renamingPath;
		const previousSelectedPaths = this.getSelectedPaths();
		const previousSelectionAnchorPath = this.#selectionAnchorPath;
		this.#unsubscribe?.();
		this.#store = nextStore;
		this.#itemHandles.clear();
		this.#invalidateKnownPathCaches();
		const nextSelectedPaths = previousSelectedPaths.map((selectedPath) => nextStore.getPathInfo(selectedPath)?.path ?? null).filter((resolved) => resolved != null);
		const selectionChanged = !arePathSetsEqual(this.#selectedPaths, nextSelectedPaths);
		this.#selectedPaths = new Set(nextSelectedPaths);
		if (selectionChanged) this.#selectionVersion += 1;
		this.#selectionAnchorPath = previousSelectionAnchorPath == null ? null : nextStore.getPathInfo(previousSelectionAnchorPath)?.path ?? null;
		this.#renamingPath = previousRenamingPath == null ? null : nextStore.getPathInfo(previousRenamingPath)?.path ?? null;
		if (this.#renamingPath == null) {
			this.#renamingValue = "";
			this.#removeRenamingPathIfCanceled = false;
		}
		this.#rebuildVisibleProjection(previousFocusedPath, previousFocusedPath != null || nextSelectedPaths.length > 0 || this.#selectionAnchorPath != null);
		this.#unsubscribe = this.#subscribe();
		this.#emit();
		this.#emitMutation({
			canonicalChanged: true,
			operation: "reset",
			pathCountAfter: resolvedInput.paths.length,
			pathCountBefore: previousPathCount,
			projectionChanged: true,
			usedPreparedInput: options.preparedInput != null,
			visibleCountDelta: this.#visibleCount - previousVisibleCount
		});
	}
	#findNearestVisibleSiblingPath(path) {
		this.#ensureFullProjection();
		const parentPath = getImmediateParentPath(path);
		const candidateKey = getSiblingComparisonKey(path, parentPath);
		let previousSiblingPath = null;
		let nextSiblingPath = null;
		for (const siblingPath of this.#getCurrentVisiblePaths()) {
			if (getImmediateParentPath(siblingPath) !== parentPath) continue;
			const siblingKey = getSiblingComparisonKey(siblingPath, parentPath);
			if (siblingKey < candidateKey) {
				previousSiblingPath = siblingPath;
				continue;
			}
			if (siblingKey > candidateKey) {
				nextSiblingPath = siblingPath;
				break;
			}
		}
		return previousSiblingPath ?? nextSiblingPath;
	}
	#resolveFocusedIndex(path) {
		const directIndex = this.#getVisibleIndexByPath(path);
		if (directIndex !== -1) return directIndex;
		const ancestorPaths = getAncestorDirectoryPaths(path);
		for (let index = ancestorPaths.length - 1; index >= 0; index -= 1) {
			const ancestorPath = ancestorPaths[index];
			if (ancestorPath == null) continue;
			const ancestorIndex = this.#getVisibleIndexByPath(ancestorPath);
			if (ancestorIndex !== -1) return ancestorIndex;
		}
		return this.#getCurrentVisiblePaths().length > 0 ? 0 : -1;
	}
	#getOrCreateItemHandle(path, itemInfo) {
		const cachedHandle = this.#itemHandles.get(path);
		if (cachedHandle != null) return cachedHandle;
		const resolvedItemInfo = itemInfo ?? this.#store.getPathInfo(path);
		if (resolvedItemInfo == null) return null;
		const handle = resolvedItemInfo.kind === "directory" ? this.#createDirectoryHandle(resolvedItemInfo.path) : this.#createFileHandle(resolvedItemInfo.path);
		this.#itemHandles.set(resolvedItemInfo.path, handle);
		return handle;
	}
	#createVisibleRow(row, visibleIndex, projectionIndex, projection) {
		return {
			ancestorPaths: projection.ancestorPaths,
			depth: row.depth,
			flattenedSegments: row.flattenedSegments?.map((segment) => ({
				isTerminal: segment.isTerminal,
				name: segment.name,
				path: segment.path
			})),
			hasChildren: row.hasChildren,
			index: visibleIndex,
			isExpanded: row.isExpanded,
			isFlattened: row.isFlattened,
			isFocused: projection.path === this.#focusedPath,
			isSelected: this.#selectedPaths.has(projection.path),
			kind: row.kind,
			level: row.depth,
			name: row.name,
			path: projection.path,
			posInSet: projection.posInSet ?? this.#projectionPosInSetByIndex[projectionIndex] ?? 0,
			setSize: projection.setSize ?? this.#projectionSetSizeByIndex[projectionIndex] ?? 0
		};
	}
	#createVisibleRowFromContext(context) {
		return this.#createVisibleRow(context.row, context.index, context.index, {
			ancestorPaths: context.ancestorPaths,
			path: context.row.path,
			posInSet: context.posInSet,
			setSize: context.setSize
		});
	}
	#getStickyCandidateContextAt(index, slotDepth) {
		const context = this.#store.getVisibleRowContext(index);
		if (context == null) return;
		const ancestorRow = context.ancestorRows[slotDepth];
		if (ancestorRow != null) return ancestorRow;
		return slotDepth === context.ancestorRows.length && context.row.kind === "directory" && context.row.isExpanded ? context : void 0;
	}
	#getAncestorIndices(index) {
		const cached = this.#ancestorIndicesByIndex.get(index);
		if (cached != null) return cached;
		const parentIndex = this.#getParentIndexForVisibleRow(index);
		const ancestorIndices = parentIndex < 0 ? [] : [...this.#getAncestorIndices(parentIndex), parentIndex];
		this.#ancestorIndicesByIndex.set(index, ancestorIndices);
		return ancestorIndices;
	}
	#getAncestorPaths(index) {
		const cached = this.#ancestorPathsByIndex.get(index);
		if (cached != null) return cached;
		const ancestorPaths = this.#getAncestorIndices(index).map((ancestorIndex) => this.#projectionPaths[ancestorIndex] ?? "").filter((path) => path !== "");
		this.#ancestorPathsByIndex.set(index, ancestorPaths);
		return ancestorPaths;
	}
	#collapseDirectory(path) {
		this.#store.collapse(path);
	}
	#applySelection(nextSelectedPaths, nextAnchorPath = this.#selectionAnchorPath, emit = true) {
		const uniqueSelectedPaths = [...new Set(nextSelectedPaths)];
		const selectionChanged = !arePathSetsEqual(this.#selectedPaths, uniqueSelectedPaths);
		const anchorChanged = this.#selectionAnchorPath !== nextAnchorPath;
		if (!selectionChanged && !anchorChanged) return;
		this.#selectedPaths = new Set(uniqueSelectedPaths);
		this.#selectionAnchorPath = nextAnchorPath;
		if (selectionChanged) this.#selectionVersion += 1;
		if (emit) this.#emit();
	}
	#createDirectoryHandle(path) {
		return {
			collapse: () => {
				this.#collapseDirectory(path);
			},
			deselect: () => {
				this.deselectPath(path);
			},
			expand: () => {
				this.#expandDirectory(path);
			},
			focus: () => {
				this.focusPath(path);
			},
			getPath: () => path,
			isDirectory: () => true,
			isExpanded: () => this.#store.isExpanded(path),
			isFocused: () => this.#focusedPath === path,
			isSelected: () => this.#selectedPaths.has(path),
			select: () => {
				this.selectPath(path);
			},
			toggleSelect: () => {
				this.togglePathSelection(path);
			},
			toggle: () => {
				this.#toggleDirectory(path);
			}
		};
	}
	#createFileHandle(path) {
		return {
			deselect: () => {
				this.deselectPath(path);
			},
			focus: () => {
				this.focusPath(path);
			},
			getPath: () => path,
			isDirectory: () => false,
			isFocused: () => this.#focusedPath === path,
			isSelected: () => this.#selectedPaths.has(path),
			select: () => {
				this.selectPath(path);
			},
			toggleSelect: () => {
				this.togglePathSelection(path);
			}
		};
	}
	#validateBatchDropOperations(operations) {
		const currentPaths = this.#store.list();
		this.#createStore(currentPaths).batch(operations);
	}
	#createStore(paths, preparedInput, initialExpandedPathsOverride) {
		return new PathStore({
			...this.#baseOptions,
			paths,
			preparedInput: preparedInput == null ? void 0 : preparedInput,
			...initialExpandedPathsOverride !== void 0 ? { initialExpandedPaths: initialExpandedPathsOverride } : {}
		});
	}
	#getListedPaths() {
		if (this.#listedPaths != null) return this.#listedPaths;
		this.#listedPaths = this.#store.list();
		return this.#listedPaths;
	}
	#getAllKnownPaths() {
		if (this.#knownPaths != null) return this.#knownPaths;
		const knownPaths = /* @__PURE__ */ new Set();
		for (const path of this.#getListedPaths()) {
			knownPaths.add(path);
			for (const ancestorPath of getAncestorDirectoryPaths(path)) knownPaths.add(ancestorPath);
		}
		this.#knownPaths = [...knownPaths].sort();
		return this.#knownPaths;
	}
	#getListedPathsLowerCase() {
		if (this.#listedPathsLowerCase != null) return this.#listedPathsLowerCase;
		this.#listedPathsLowerCase = this.#getListedPaths().map(toLowerCaseSearchPath);
		return this.#listedPathsLowerCase;
	}
	#getAllKnownDirectoryPaths() {
		if (this.#knownDirectoryPaths != null) return this.#knownDirectoryPaths;
		this.#knownDirectoryPaths = this.#getAllKnownPaths().filter((path) => path.endsWith("/"));
		return this.#knownDirectoryPaths;
	}
	#getAllKnownDirectoryPathsLowerCase() {
		if (this.#knownDirectoryPathsLowerCase != null) return this.#knownDirectoryPathsLowerCase;
		this.#knownDirectoryPathsLowerCase = this.#getAllKnownDirectoryPaths().map(toLowerCaseSearchPath);
		return this.#knownDirectoryPathsLowerCase;
	}
	#invalidateKnownPathCaches() {
		this.#knownDirectoryPaths = null;
		this.#knownDirectoryPathsLowerCase = null;
		this.#knownPaths = null;
		this.#listedPaths = null;
		this.#listedPathsLowerCase = null;
	}
	#getExpandedDirectoryPaths() {
		return this.#getAllKnownDirectoryPaths().filter((path) => this.#store.isExpanded(path));
	}
	#restoreSearchExpandedPaths(keepSelectedOpen) {
		const expandedPaths = new Set(this.#searchPreviousExpandedPaths ?? []);
		if (keepSelectedOpen) for (const selectedPath of this.#selectedPaths) for (const ancestorPath of getAncestorDirectoryPaths(selectedPath)) expandedPaths.add(ancestorPath);
		this.#setExpandedPaths(expandedPaths);
	}
	#setExpandedPaths(expandedPaths) {
		this.#suppressStoreNotifications = true;
		try {
			for (const directoryPath of this.#getAllKnownDirectoryPaths()) {
				const shouldExpand = expandedPaths.has(directoryPath);
				const isExpanded = this.#store.isExpanded(directoryPath);
				if (shouldExpand && !isExpanded) this.#store.expand(directoryPath);
				else if (!shouldExpand && isExpanded) this.#store.collapse(directoryPath);
			}
		} finally {
			this.#suppressStoreNotifications = false;
		}
	}
	#syncSearchVisibilityState() {
		if (this.#searchValue == null || this.#searchValue.length === 0) {
			this.#searchMatchingPaths = [];
			this.#searchVisibleIndices = null;
			this.#searchVisiblePaths = null;
			this.#searchVisibleIndexByPath = null;
			this.#visibleCount = this.#storeVisibleCount;
			return;
		}
		const currentVisiblePaths = this.#projectionPaths;
		this.#searchMatchingPaths = currentVisiblePaths.filter((path) => this.#searchMatchPathSet.has(path));
		if (this.#searchMode !== "hide-non-matches" || this.#searchMatchPathSet.size === 0) {
			this.#searchVisibleIndices = null;
			this.#searchVisiblePaths = null;
			this.#searchVisibleIndexByPath = null;
			this.#visibleCount = this.#storeVisibleCount;
			return;
		}
		const visibleIndices = [];
		const visiblePaths = [];
		const visibleIndexByPath = /* @__PURE__ */ new Map();
		for (const [index, path] of currentVisiblePaths.entries()) {
			if (this.#searchVisiblePathSet?.has(path) !== true) continue;
			visibleIndexByPath.set(path, visiblePaths.length);
			visibleIndices.push(index);
			visiblePaths.push(path);
		}
		this.#searchVisibleIndices = visibleIndices;
		this.#searchVisiblePaths = visiblePaths;
		this.#searchVisibleIndexByPath = visibleIndexByPath;
		this.#visibleCount = visiblePaths.length;
	}
	#getCurrentVisiblePaths() {
		return this.#searchVisiblePaths ?? this.#projectionPaths;
	}
	#getExactCurrentVisibleIndexByPath(path) {
		if (this.#searchVisiblePaths != null) return this.#searchVisibleIndexByPath?.get(path) ?? -1;
		return this.#store.getVisibleIndex(path) ?? -1;
	}
	#getProjectionIndexFromVisibleIndex(index) {
		return this.#searchVisibleIndices?.[index] ?? index;
	}
	#getVisibleIndexByPath(path) {
		const searchIndex = this.#searchVisibleIndexByPath?.get(path);
		if (searchIndex != null) return searchIndex;
		return this.#store.getVisibleIndex(path) ?? -1;
	}
	#focusRelativeSearchMatch(direction) {
		const matchPaths = this.#searchMatchingPaths;
		if (matchPaths.length === 0) return;
		const focusedPath = this.#focusedPath;
		const currentIndex = focusedPath == null ? -1 : matchPaths.indexOf(focusedPath);
		const nextPath = matchPaths[currentIndex < 0 ? direction > 0 ? 0 : matchPaths.length - 1 : Math.min(matchPaths.length - 1, Math.max(0, currentIndex + direction))];
		if (nextPath != null) this.focusPath(nextPath);
	}
	#setSearchState(value, emitChange) {
		const normalizedValue = value == null ? null : normalizeSearchQuery(value);
		const previousSearch = this.#searchValue;
		if (previousSearch === normalizedValue) return;
		if (previousSearch == null && normalizedValue != null) this.#searchPreviousExpandedPaths = this.#getExpandedDirectoryPaths();
		this.#searchValue = normalizedValue;
		if (normalizedValue == null) {
			this.#restoreSearchExpandedPaths(true);
			this.#searchPreviousExpandedPaths = null;
			this.#searchMatchPathSet.clear();
			this.#searchVisiblePathSet = null;
			this.#rebuildVisibleProjection(this.#focusedPath, true);
		} else if (normalizedValue.length === 0) {
			this.#restoreSearchExpandedPaths(false);
			this.#searchMatchPathSet.clear();
			this.#searchVisiblePathSet = null;
			this.#rebuildVisibleProjection(this.#focusedPath, true);
		} else {
			const focusCandidate = this.#refreshActiveSearchState();
			this.#rebuildVisibleProjection(focusCandidate, true);
		}
		if (emitChange) {
			this.#onSearchChange?.(this.#searchValue);
			this.#emit();
		}
	}
	#refreshActiveSearchState() {
		if (this.#searchValue == null || this.#searchValue.length === 0) {
			this.#searchMatchPathSet.clear();
			return this.#focusedPath;
		}
		const searchValue = this.#searchValue;
		const listedPaths = this.#getListedPaths();
		const listedPathsLowerCase = this.#getListedPathsLowerCase();
		const matchingPaths = [];
		const matchingPathSet = /* @__PURE__ */ new Set();
		let focusCandidate = null;
		for (let index = 0; index < listedPaths.length; index += 1) {
			if (!listedPathsLowerCase[index].includes(searchValue)) continue;
			const path = listedPaths[index];
			matchingPaths.push(path);
			matchingPathSet.add(path);
			focusCandidate ??= path;
		}
		const knownDirectoryPaths = this.#getAllKnownDirectoryPaths();
		const knownDirectoryPathsLowerCase = this.#getAllKnownDirectoryPathsLowerCase();
		for (let index = 0; index < knownDirectoryPaths.length; index += 1) {
			if (!knownDirectoryPathsLowerCase[index].includes(searchValue)) continue;
			const path = knownDirectoryPaths[index];
			if (matchingPathSet.has(path)) continue;
			matchingPaths.push(path);
			matchingPathSet.add(path);
			focusCandidate ??= path;
		}
		this.#searchMatchPathSet = matchingPathSet;
		const searchVisiblePathSet = this.#searchMode === "hide-non-matches" && matchingPaths.length > 0 ? /* @__PURE__ */ new Set() : null;
		this.#searchVisiblePathSet = searchVisiblePathSet;
		const expandedPaths = this.#searchMode === "expand-matches" ? new Set(this.#searchPreviousExpandedPaths ?? []) : /* @__PURE__ */ new Set();
		for (const matchingPath of matchingPaths) {
			if (searchVisiblePathSet != null) searchVisiblePathSet.add(matchingPath);
			if (matchingPath.endsWith("/")) expandedPaths.add(matchingPath);
			for (const ancestorPath of getAncestorDirectoryPaths(matchingPath)) {
				expandedPaths.add(ancestorPath);
				if (searchVisiblePathSet != null) searchVisiblePathSet.add(ancestorPath);
			}
		}
		this.#setExpandedPaths(expandedPaths);
		return focusCandidate ?? this.#focusedPath;
	}
	#emit() {
		for (const listener of this.#listeners) listener();
	}
	#emitMutation(event) {
		this.#mutationListeners.get(event.operation)?.forEach((listener) => {
			listener(event);
		});
		this.#mutationListeners.get("*")?.forEach((listener) => {
			listener(event);
		});
	}
	#expandDirectory(path) {
		for (const ancestorPath of getAncestorDirectoryPaths(path)) {
			if (this.#store.isExpanded(ancestorPath)) continue;
			this.#store.expand(ancestorPath);
		}
		if (!this.#store.isExpanded(path)) this.#store.expand(path);
	}
	#moveFocus(offset) {
		const itemCount = this.#visibleCount;
		if (itemCount === 0) return;
		const currentIndex = this.#focusedIndex === -1 ? 0 : this.#focusedIndex;
		const nextIndex = Math.min(itemCount - 1, Math.max(0, currentIndex + offset));
		if (nextIndex !== currentIndex || this.#focusedIndex === -1) {
			if (!this.#hasFullProjection && this.#searchVisibleIndices == null && nextIndex >= this.#projectionPaths.length) this.#ensureFullProjection();
			this.#setFocusedIndex(nextIndex);
		}
	}
	#rebuildVisibleProjection(focusedPathCandidate, full = true) {
		const rawVisibleCount = this.#store.getVisibleCount();
		this.#storeVisibleCount = rawVisibleCount;
		const projection = createVisibleProjection(this.#store.getVisibleTreeProjectionData(full ? void 0 : Math.min(rawVisibleCount, INITIAL_PROJECTION_ROW_LIMIT)), focusedPathCandidate, full ? (path) => this.#store.getVisibleIndex(path) : void 0);
		this.#ancestorIndicesByIndex.clear();
		this.#ancestorPathsByIndex.clear();
		this.#hasFullProjection = projection.paths.length >= rawVisibleCount;
		this.#getParentIndexForVisibleRow = projection.getParentIndex;
		this.#projectionPaths = projection.paths;
		this.#projectionPosInSetByIndex = projection.posInSetByIndex;
		this.#projectionSetSizeByIndex = projection.setSizeByIndex;
		this.#syncSearchVisibilityState();
		this.#focusedIndex = focusedPathCandidate == null ? this.#getCurrentVisiblePaths().length > 0 ? 0 : -1 : this.#resolveFocusedIndex(focusedPathCandidate);
		this.#focusedPath = this.#focusedIndex < 0 ? null : this.#resolveVisiblePathAtIndex(this.#focusedIndex);
	}
	#resolveVisiblePathAtIndex(index) {
		const projectedPath = this.#getCurrentVisiblePaths()[index];
		if (projectedPath != null) return projectedPath;
		if (this.#searchVisibleIndices != null) return null;
		return this.#store.getVisibleRowContext(index)?.row.path ?? null;
	}
	#resolveSelectionPath(path) {
		return this.#store.getPathInfo(path)?.path ?? null;
	}
	#focusPathWithoutEmit(path) {
		if (path == null) return;
		const nextFocusedIndex = this.#resolveFocusedIndex(path);
		if (nextFocusedIndex >= 0) this.#setFocusedIndex(nextFocusedIndex, false);
	}
	#setFocusedIndex(index, emit = true) {
		const nextPath = this.#resolveVisiblePathAtIndex(index);
		if (nextPath == null) return;
		if (this.#focusedIndex === index && this.#focusedPath === nextPath) return;
		this.#focusedIndex = index;
		this.#focusedPath = nextPath;
		if (emit) this.#emit();
	}
	#ensureFullProjection() {
		if (this.#hasFullProjection) return;
		this.#rebuildVisibleProjection(this.#focusedPath, true);
	}
	#applyMutationState(event) {
		const nextRenamingPath = remapPathThroughMutation(this.#renamingPath, event);
		if (nextRenamingPath == null && this.#renamingPath != null) this.#renamingValue = "";
		this.#renamingPath = nextRenamingPath;
		const nextFocusedPath = remapPathThroughMutation(this.#focusedPath, event, true);
		const nextSelectedPaths = [...this.#selectedPaths].map((selectedPath) => remapPathThroughMutation(selectedPath, event)).filter((resolvedPath) => resolvedPath != null).map((resolvedPath) => this.#store.getPathInfo(resolvedPath)?.path ?? null).filter((resolvedPath) => resolvedPath != null);
		const nextSelectionAnchorPath = remapPathThroughMutation(this.#selectionAnchorPath, event);
		const canonicalAnchorPath = nextSelectionAnchorPath == null ? null : this.#store.getPathInfo(nextSelectionAnchorPath)?.path ?? null;
		const uniqueNextSelectedPaths = [...new Set(nextSelectedPaths)];
		if (!arePathSetsEqual(this.#selectedPaths, uniqueNextSelectedPaths)) {
			this.#selectedPaths = new Set(uniqueNextSelectedPaths);
			this.#selectionVersion += 1;
		}
		this.#selectionAnchorPath = canonicalAnchorPath;
		return nextFocusedPath;
	}
	#subscribe() {
		return this.#store.on("*", (event) => {
			if (this.#suppressStoreNotifications) return;
			if (event.canonicalChanged) {
				this.#itemHandles.clear();
				this.#invalidateKnownPathCaches();
			}
			if (this.#dragSession != null && isPathMutationEvent(event)) this.#dragSession = null;
			const focusPathCandidate = isPathMutationEvent(event) ? this.#applyMutationState(event) : this.#focusedPath;
			const searchFocusCandidate = this.#searchValue != null && this.#searchValue.length > 0 ? this.#refreshActiveSearchState() : this.#searchValue === "" ? this.#focusedPath : focusPathCandidate;
			const shouldBuildFullProjection = this.#searchValue != null || event.operation !== "expand" && event.operation !== "collapse";
			this.#rebuildVisibleProjection(searchFocusCandidate, shouldBuildFullProjection);
			this.#emit();
			const mutationEvent = toTreesMutationEvent(event);
			if (mutationEvent != null) this.#emitMutation(mutationEvent);
		});
	}
	#toggleDirectory(path) {
		if (this.#store.isExpanded(path)) {
			this.#collapseDirectory(path);
			return;
		}
		this.#expandDirectory(path);
	}
};

//#endregion
export { FILE_TREE_RENAME_VIEW, FileTreeController };
//# sourceMappingURL=FileTreeController.js.map