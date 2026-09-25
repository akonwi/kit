import { PATH_STORE_CHILD_INDEX_CHUNK_THRESHOLD_EXTERNAL, rebuildVisibleChildChunks } from "./child-index.js";
import { getNodeDepth, isDirectoryNode } from "./internal-types.js";
import { getBenchmarkInstrumentation, withBenchmarkPhase } from "./internal/benchmarkInstrumentation.js";
import { compareSegmentSortKeys, createSegmentSortKey, getSegmentSortKey } from "./sort.js";
import { PathStoreBuilder, getPreparedInputEntries, getPreparedInputPresortedPaths, getPreparedInputPresortedPathsContainDirectories, prepareInput, preparePathEntries, preparePaths, preparePresortedInput } from "./builder.js";
import { beginDirectoryLoad, completeDirectoryLoad, createPathStoreState, failDirectoryLoad, getDirectoryLoadState, isDirectoryExpanded, isDirectoryLoadAttemptCurrent, markDirectoryUnloadedState, setDirectoryExpanded } from "./state.js";
import { batchEvents, createApplyChildPatchEvent, createBeginChildLoadEvent, createCleanupEvent, createCompleteChildLoadEvent, createFailChildLoadEvent, createMarkDirectoryUnloadedEvent, finalizeEvent, recordEvent, subscribe } from "./events.js";
import { getFlattenedChildDirectoryId } from "./flatten.js";
import { addPath, collectAncestorIds, findNodeId, getDirectoryIndex, listPaths, materializeNodePath, movePath, recomputeCountsRecursive, removePath, requireNode } from "./canonical.js";
import { cleanupPathStoreState, hasActiveCleanupBlockingLoads } from "./cleanup.js";
import { collapsePath, expandPath, getVisibleCount, getVisibleIndexByPath, getVisibleRowContext, getVisibleSlice, getVisibleTreeProjection, getVisibleTreeProjectionData } from "./projection.js";

//#region ../path-store/src/store.ts
function initializeOpenVisibleCounts(state) {
	const { directories, nodes, options, rootId, presortedDirectoryNodeIds } = state.snapshot;
	const flattenEmptyDirectories = options.flattenEmptyDirectories === true;
	const walkDirectory = (nodeId) => {
		const currentNode = nodes[nodeId];
		if (currentNode == null || !isDirectoryNode(currentNode)) return;
		const currentIndex = directories.get(nodeId);
		if (currentIndex == null) throw new Error(`Unknown directory child index for node ${String(nodeId)}`);
		const childIds = currentIndex.childIds;
		const childCount = childIds.length;
		let totalChildSubtreeNodeCount = 0;
		let totalChildVisibleSubtreeCount = 0;
		for (let ci = 0; ci < childCount; ci++) {
			const childId = childIds[ci];
			if (childId == null) continue;
			const childNode = nodes[childId];
			totalChildSubtreeNodeCount += childNode.subtreeNodeCount;
			totalChildVisibleSubtreeCount += childNode.visibleSubtreeCount;
		}
		currentIndex.totalChildSubtreeNodeCount = totalChildSubtreeNodeCount;
		currentIndex.totalChildVisibleSubtreeCount = totalChildVisibleSubtreeCount;
		if (childCount >= PATH_STORE_CHILD_INDEX_CHUNK_THRESHOLD_EXTERNAL) rebuildVisibleChildChunks(nodes, currentIndex);
		currentNode.subtreeNodeCount = 1 + totalChildSubtreeNodeCount;
		let newVisibleSubtreeCount;
		if (flattenEmptyDirectories && childCount === 1) {
			const onlyChild = nodes[childIds[0]];
			newVisibleSubtreeCount = onlyChild != null && isDirectoryNode(onlyChild) ? totalChildVisibleSubtreeCount : 1 + totalChildVisibleSubtreeCount;
		} else newVisibleSubtreeCount = 1 + totalChildVisibleSubtreeCount;
		currentNode.visibleSubtreeCount = newVisibleSubtreeCount;
	};
	if (presortedDirectoryNodeIds != null) for (let i = presortedDirectoryNodeIds.length - 1; i >= 0; i--) walkDirectory(presortedDirectoryNodeIds[i]);
	else for (let nodeId = nodes.length - 1; nodeId >= 1; nodeId--) walkDirectory(nodeId);
	const rootNode = nodes[rootId];
	const rootIndex = directories.get(rootId);
	if (rootNode == null || rootIndex == null) return;
	const rootChildIds = rootIndex.childIds;
	let rootTotalChildSubtreeNodeCount = 0;
	let rootTotalChildVisibleSubtreeCount = 0;
	for (let ci = 0; ci < rootChildIds.length; ci++) {
		const childId = rootChildIds[ci];
		if (childId == null) continue;
		const childNode = nodes[childId];
		rootTotalChildSubtreeNodeCount += childNode.subtreeNodeCount;
		rootTotalChildVisibleSubtreeCount += childNode.visibleSubtreeCount;
	}
	rootIndex.totalChildSubtreeNodeCount = rootTotalChildSubtreeNodeCount;
	rootIndex.totalChildVisibleSubtreeCount = rootTotalChildVisibleSubtreeCount;
	rebuildVisibleChildChunks(nodes, rootIndex);
	rootNode.subtreeNodeCount = 1 + rootTotalChildSubtreeNodeCount;
	rootNode.visibleSubtreeCount = rootTotalChildVisibleSubtreeCount;
}
function canInitializeOpenVisibleCounts(options) {
	return options.initialExpansion === "open" && (options.initialExpandedPaths == null || options.initialExpandedPaths.length === 0);
}
var PathStore = class PathStore {
	#state;
	constructor(options = {}) {
		const instrumentation = getBenchmarkInstrumentation(options);
		const builder = withBenchmarkPhase(instrumentation, "store.builder.create", () => new PathStoreBuilder(options));
		if (options.preparedInput != null) {
			const presortedPaths = getPreparedInputPresortedPaths(options.preparedInput);
			if (presortedPaths != null) builder.appendPresortedPaths(presortedPaths, getPreparedInputPresortedPathsContainDirectories(options.preparedInput));
			else builder.appendPreparedPaths(getPreparedInputEntries(options.preparedInput), false);
		} else {
			const inputPaths = options.paths ?? [];
			if (options.presorted === true) builder.appendPaths(inputPaths);
			else builder.appendPreparedPaths(withBenchmarkPhase(instrumentation, "store.preparePathEntries", () => preparePathEntries(inputPaths, options)));
		}
		const snapshot = withBenchmarkPhase(instrumentation, "store.builder.finish", () => builder.finish({ skipSubtreeCountPass: true }));
		const useExplicitOpenExpansionFastPath = withBenchmarkPhase(instrumentation, "store.state.detectAllDirectoriesExpanded", () => (options.initialExpansion ?? "closed") === "closed" && builder.didMatchAllInitialExpandedPaths());
		this.#state = withBenchmarkPhase(instrumentation, "store.state.create", () => createPathStoreState(snapshot, useExplicitOpenExpansionFastPath ? "open" : options.initialExpansion ?? "closed", instrumentation));
		if (useExplicitOpenExpansionFastPath) this.#state.collapseNewDirectoriesByDefault = true;
		const expandedDirectoryCount = useExplicitOpenExpansionFastPath ? this.#state.snapshot.directories.size - 1 : withBenchmarkPhase(instrumentation, "store.state.initializeExpandedPaths", () => this.initializeExpandedPaths(options.initialExpandedPaths));
		if (useExplicitOpenExpansionFastPath || canInitializeOpenVisibleCounts(options) || (options.initialExpansion ?? "closed") === "closed" && expandedDirectoryCount === this.#state.snapshot.directories.size - 1 || (options.initialExpandedPaths?.length ?? 0) > 0 && withBenchmarkPhase(instrumentation, "store.state.checkAllDirectoriesExpanded", () => this.hasAllDirectoriesExpanded())) withBenchmarkPhase(instrumentation, "store.state.initializeOpenVisibleCounts", () => initializeOpenVisibleCounts(this.#state));
		else withBenchmarkPhase(instrumentation, "store.state.recomputeCounts", () => recomputeCountsRecursive(this.#state, this.#state.snapshot.rootId));
	}
	static preparePaths(paths, options = {}) {
		return preparePaths(paths, options);
	}
	static prepareInput(paths, options = {}) {
		return prepareInput(paths, options);
	}
	static preparePresortedInput(paths) {
		return preparePresortedInput(paths);
	}
	list(path) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.list", () => listPaths(this.#state, path));
	}
	add(path) {
		withBenchmarkPhase(this.#state.instrumentation, "store.add", () => {
			const previousVisibleCount = getVisibleCount(this.#state);
			recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, addPath(this.#state, path)));
		});
	}
	remove(path, options = {}) {
		withBenchmarkPhase(this.#state.instrumentation, "store.remove", () => {
			const previousVisibleCount = getVisibleCount(this.#state);
			recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, removePath(this.#state, path, options)));
		});
	}
	move(fromPath, toPath, options = {}) {
		withBenchmarkPhase(this.#state.instrumentation, "store.move", () => {
			const previousVisibleCount = getVisibleCount(this.#state);
			const event = movePath(this.#state, fromPath, toPath, options);
			if (event != null) recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, event));
		});
	}
	batch(operations) {
		batchEvents(this.#state, () => {
			if (typeof operations === "function") {
				operations(this);
				return;
			}
			for (const operation of operations) switch (operation.type) {
				case "add":
					this.add(operation.path);
					break;
				case "remove":
					this.remove(operation.path, { recursive: operation.recursive });
					break;
				case "move":
					this.move(operation.from, operation.to, { collision: operation.collision });
					break;
			}
		});
	}
	getVisibleCount() {
		return withBenchmarkPhase(this.#state.instrumentation, "store.getVisibleCount", () => getVisibleCount(this.#state));
	}
	getVisibleSlice(start, end) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.getVisibleSlice", () => getVisibleSlice(this.#state, start, end));
	}
	getVisibleRowContext(index) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.getVisibleRowContext", () => getVisibleRowContext(this.#state, index));
	}
	getVisibleTreeProjection() {
		return getVisibleTreeProjection(this.#state);
	}
	getVisibleTreeProjectionData(maxRows) {
		return getVisibleTreeProjectionData(this.#state, maxRows);
	}
	/**
	* Resolves a path to its visible row index without building a full projection
	* index. Returns null when the path is unknown or currently hidden.
	*/
	getVisibleIndex(path) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.getVisibleIndex", () => getVisibleIndexByPath(this.#state, path));
	}
	/**
	* Resolves a lookup path to the store's canonical path and item kind.
	* Lets tree adapters answer path-first queries without building a second
	* whole-tree metadata index alongside the store.
	*/
	getPathInfo(path) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.getPathInfo", () => {
			const nodeId = findNodeId(this.#state, path);
			if (nodeId == null) return null;
			const node = requireNode(this.#state, nodeId);
			return {
				depth: getNodeDepth(node),
				kind: isDirectoryNode(node) ? "directory" : "file",
				path: materializeNodePath(this.#state, nodeId)
			};
		});
	}
	isExpanded(path) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.isExpanded", () => {
			const directoryNodeId = this.requireDirectoryNodeId(path);
			const directoryNode = requireNode(this.#state, directoryNodeId);
			return isDirectoryExpanded(this.#state, directoryNodeId, directoryNode);
		});
	}
	expand(path) {
		withBenchmarkPhase(this.#state.instrumentation, "store.expand", () => {
			const previousVisibleCount = getVisibleCount(this.#state);
			const event = expandPath(this.#state, path);
			if (event != null) recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, event));
		});
	}
	collapse(path) {
		withBenchmarkPhase(this.#state.instrumentation, "store.collapse", () => {
			const previousVisibleCount = getVisibleCount(this.#state);
			const event = collapsePath(this.#state, path);
			if (event != null) recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, event));
		});
	}
	on(type, handler) {
		return subscribe(this.#state, type, handler);
	}
	getDirectoryLoadState(path) {
		const directoryNodeId = this.requireDirectoryNodeId(path);
		return getDirectoryLoadState(this.#state, directoryNodeId);
	}
	markDirectoryUnloaded(path) {
		withBenchmarkPhase(this.#state.instrumentation, "store.markDirectoryUnloaded", () => {
			const directoryNodeId = this.requireDirectoryNodeId(path);
			if (getDirectoryIndex(this.#state, directoryNodeId).childIds.length > 0) throw new Error(`Cannot mark a directory with known children as unloaded: "${path}"`);
			const previousVisibleCount = getVisibleCount(this.#state);
			markDirectoryUnloadedState(this.#state, directoryNodeId);
			recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, createMarkDirectoryUnloadedEvent({
				affectedAncestorIds: collectAncestorIds(this.#state, directoryNodeId),
				affectedNodeIds: [directoryNodeId],
				path,
				projectionChanged: this.isDirectoryProjectionVisible(directoryNodeId)
			})));
		});
	}
	beginChildLoad(path) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.beginChildLoad", () => {
			const directoryNodeId = this.requireDirectoryNodeId(path);
			const previousVisibleCount = getVisibleCount(this.#state);
			const attempt = beginDirectoryLoad(this.#state, directoryNodeId);
			recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, createBeginChildLoadEvent({
				affectedAncestorIds: collectAncestorIds(this.#state, directoryNodeId),
				affectedNodeIds: [directoryNodeId],
				attemptId: attempt.attemptId,
				path,
				projectionChanged: this.isDirectoryProjectionVisible(directoryNodeId),
				reused: attempt.reused
			})));
			return attempt;
		});
	}
	applyChildPatch(attempt, patch) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.applyChildPatch", () => {
			const directoryNodeId = this.resolveActiveDirectoryNodeId(attempt.nodeId);
			if (directoryNodeId == null || getDirectoryLoadState(this.#state, directoryNodeId) !== "loading" || !isDirectoryLoadAttemptCurrent(this.#state, directoryNodeId, attempt.attemptId)) return false;
			const directoryPath = materializeNodePath(this.#state, directoryNodeId);
			this.validateChildPatch(directoryPath, patch);
			const previousVisibleCount = getVisibleCount(this.#state);
			const childEvents = [];
			for (const operation of patch.operations) {
				assertOperationTargetsDirectory(directoryPath, operation);
				const operationVisibleCount = getVisibleCount(this.#state);
				switch (operation.type) {
					case "add":
						childEvents.push(finalizeEvent(this.#state, operationVisibleCount, addPath(this.#state, operation.path)));
						break;
					case "remove":
						childEvents.push(finalizeEvent(this.#state, operationVisibleCount, removePath(this.#state, operation.path, { recursive: operation.recursive })));
						break;
					case "move": {
						const event = movePath(this.#state, operation.from, operation.to, { collision: operation.collision });
						if (event != null) childEvents.push(finalizeEvent(this.#state, operationVisibleCount, event));
						break;
					}
				}
			}
			const projectionChanged = childEvents.some((event) => event.projectionChanged) || this.isDirectoryProjectionVisible(directoryNodeId);
			recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, createApplyChildPatchEvent({
				affectedAncestorIds: collectAncestorIds(this.#state, directoryNodeId),
				affectedNodeIds: [directoryNodeId],
				attemptId: attempt.attemptId,
				childEvents,
				path: materializeNodePath(this.#state, directoryNodeId),
				projectionChanged
			})));
			return true;
		});
	}
	completeChildLoad(attempt) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.completeChildLoad", () => {
			const directoryNodeId = this.resolveActiveDirectoryNodeId(attempt.nodeId);
			if (directoryNodeId == null) return false;
			const previousVisibleCount = getVisibleCount(this.#state);
			const applied = completeDirectoryLoad(this.#state, directoryNodeId, attempt.attemptId);
			recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, createCompleteChildLoadEvent({
				affectedAncestorIds: collectAncestorIds(this.#state, directoryNodeId),
				affectedNodeIds: [directoryNodeId],
				attemptId: attempt.attemptId,
				path: materializeNodePath(this.#state, directoryNodeId),
				projectionChanged: this.isDirectoryProjectionVisible(directoryNodeId),
				stale: !applied
			})));
			return applied;
		});
	}
	failChildLoad(attempt, errorMessage) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.failChildLoad", () => {
			const directoryNodeId = this.resolveActiveDirectoryNodeId(attempt.nodeId);
			if (directoryNodeId == null) return false;
			const previousVisibleCount = getVisibleCount(this.#state);
			const applied = failDirectoryLoad(this.#state, directoryNodeId, attempt.attemptId, errorMessage);
			recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, createFailChildLoadEvent({
				affectedAncestorIds: collectAncestorIds(this.#state, directoryNodeId),
				affectedNodeIds: [directoryNodeId],
				attemptId: attempt.attemptId,
				errorMessage,
				path: materializeNodePath(this.#state, directoryNodeId),
				projectionChanged: this.isDirectoryProjectionVisible(directoryNodeId),
				stale: !applied
			})));
			return applied;
		});
	}
	cleanup(options = {}) {
		return withBenchmarkPhase(this.#state.instrumentation, "store.cleanup", () => {
			if (this.#state.transactionStack.length > 0) throw new Error("Cleanup cannot run during an open batch or transaction.");
			if (hasActiveCleanupBlockingLoads(this.#state)) throw new Error("Cleanup cannot run while directory loads are active.");
			const previousVisibleCount = getVisibleCount(this.#state);
			const result = cleanupPathStoreState(this.#state, options.mode ?? "stable");
			recordEvent(this.#state, finalizeEvent(this.#state, previousVisibleCount, createCleanupEvent({
				...result,
				affectedAncestorIds: [],
				affectedNodeIds: [],
				projectionChanged: result.idsPreserved === false
			})));
			return result;
		});
	}
	getNodeCount() {
		return this.#state.activeNodeCount;
	}
	initializeExpandedPaths(expandedPaths) {
		if (expandedPaths == null || expandedPaths.length === 0) return 0;
		let expandedDirectoryCount = 0;
		const previousChildOffsets = [];
		const previousNodeIds = [];
		let previousEndIndex = 0;
		let previousPath = null;
		const segmentTable = this.#state.snapshot.segmentTable;
		const segmentValues = segmentTable.valueById;
		const nodes = this.#state.snapshot.nodes;
		const targetSegmentSortKeyCache = /* @__PURE__ */ new Map();
		for (const path of expandedPaths) {
			if (previousPath != null && path < previousPath) {
				previousPath = null;
				previousEndIndex = 0;
				previousChildOffsets.length = 0;
				previousNodeIds.length = 0;
			}
			const endIndex = path.length > 0 && path.charCodeAt(path.length - 1) === 47 ? path.length - 1 : path.length;
			if (endIndex === 0) {
				previousPath = path;
				previousEndIndex = endIndex;
				previousChildOffsets.length = 0;
				previousNodeIds.length = 0;
				continue;
			}
			let sharedDepth = 0;
			let unsharedSegmentStart = 0;
			if (previousPath != null) {
				const compareLength = Math.min(endIndex, previousEndIndex);
				let prefixMatched = true;
				for (let charIndex = 0; charIndex < compareLength; charIndex += 1) {
					const charCode = path.charCodeAt(charIndex);
					if (charCode !== previousPath.charCodeAt(charIndex)) {
						prefixMatched = false;
						break;
					}
					if (charCode === 47) {
						sharedDepth += 1;
						unsharedSegmentStart = charIndex + 1;
					}
				}
				if (prefixMatched) {
					if (compareLength === previousEndIndex && endIndex > compareLength && path.charCodeAt(compareLength) === 47) {
						sharedDepth += 1;
						unsharedSegmentStart = compareLength + 1;
					} else if (compareLength === endIndex && previousEndIndex > compareLength && previousPath.charCodeAt(compareLength) === 47) {
						sharedDepth += 1;
						unsharedSegmentStart = endIndex + 1;
					}
				}
				sharedDepth = Math.min(sharedDepth, previousNodeIds.length);
			}
			let currentDirectoryId = sharedDepth === 0 ? this.#state.snapshot.rootId : previousNodeIds[sharedDepth - 1] ?? this.#state.snapshot.rootId;
			let resolvedDepth = sharedDepth;
			let foundDirectory = true;
			let segmentStart = unsharedSegmentStart;
			while (segmentStart <= endIndex) {
				const slashIndex = path.indexOf("/", segmentStart);
				const segmentEnd = slashIndex === -1 || slashIndex > endIndex ? endIndex : slashIndex;
				const segment = path.slice(segmentStart, segmentEnd);
				const childIds = getDirectoryIndex(this.#state, currentDirectoryId).childIds;
				const searchStartIndex = resolvedDepth === sharedDepth ? previousChildOffsets[resolvedDepth] ?? 0 : 0;
				let nextChildOffset = searchStartIndex;
				let nextNodeId;
				const targetSegmentSortKey = targetSegmentSortKeyCache.get(segment) ?? createSegmentSortKey(segment);
				targetSegmentSortKeyCache.set(segment, targetSegmentSortKey);
				const searchForSegment = (startIndex, endIndex$1) => {
					for (nextChildOffset = startIndex; nextChildOffset < endIndex$1; nextChildOffset += 1) {
						const candidateNodeId = childIds[nextChildOffset];
						const candidateNode = nodes[candidateNodeId];
						const candidateSegment = segmentValues[candidateNode.nameId];
						if (candidateSegment === segment) {
							nextNodeId = candidateNodeId;
							return true;
						}
						const orderComparison = compareSegmentSortKeys(getSegmentSortKey(segmentTable, candidateNode.nameId), targetSegmentSortKey);
						if (orderComparison > 0 || orderComparison === 0 && candidateSegment > segment) return false;
					}
					return false;
				};
				if (!searchForSegment(searchStartIndex, childIds.length) && searchStartIndex > 0) searchForSegment(0, searchStartIndex);
				if (nextNodeId === void 0) {
					foundDirectory = false;
					break;
				}
				if (!isDirectoryNode(requireNode(this.#state, nextNodeId))) {
					foundDirectory = false;
					break;
				}
				previousChildOffsets[resolvedDepth] = nextChildOffset;
				previousNodeIds[resolvedDepth] = nextNodeId;
				currentDirectoryId = nextNodeId;
				resolvedDepth += 1;
				if (segmentEnd === endIndex) break;
				segmentStart = segmentEnd + 1;
			}
			previousPath = path;
			previousEndIndex = endIndex;
			previousChildOffsets.length = resolvedDepth;
			previousNodeIds.length = resolvedDepth;
			if (!foundDirectory) {
				previousPath = null;
				previousEndIndex = 0;
				previousChildOffsets.length = 0;
				previousNodeIds.length = 0;
				continue;
			}
			for (let depthIndex = sharedDepth; depthIndex < resolvedDepth; depthIndex += 1) {
				const directoryNodeId = previousNodeIds[depthIndex];
				if (directoryNodeId == null) continue;
				const directoryNode = requireNode(this.#state, directoryNodeId);
				if (isDirectoryExpanded(this.#state, directoryNodeId, directoryNode)) continue;
				setDirectoryExpanded(this.#state, directoryNodeId, true, directoryNode);
				expandedDirectoryCount += 1;
			}
		}
		return expandedDirectoryCount;
	}
	hasAllDirectoriesExpanded() {
		for (const directoryNodeId of this.#state.snapshot.directories.keys()) {
			if (directoryNodeId === this.#state.snapshot.rootId) continue;
			const directoryNode = requireNode(this.#state, directoryNodeId);
			if (!isDirectoryExpanded(this.#state, directoryNodeId, directoryNode)) return false;
		}
		return true;
	}
	requireDirectoryNodeId(path) {
		const directoryNodeId = findNodeId(this.#state, path);
		if (directoryNodeId == null) throw new Error(`Path does not exist: "${path}"`);
		if (!isDirectoryNode(requireNode(this.#state, directoryNodeId))) throw new Error(`Path is not a directory: "${path}"`);
		return directoryNodeId;
	}
	resolveActiveDirectoryNodeId(directoryNodeId) {
		try {
			if (!isDirectoryNode(requireNode(this.#state, directoryNodeId))) throw new Error(`Node is not a directory: ${String(directoryNodeId)}`);
			return directoryNodeId;
		} catch {
			return null;
		}
	}
	isDirectoryProjectionVisible(directoryNodeId) {
		let currentNodeId = directoryNodeId;
		while (currentNodeId !== this.#state.snapshot.rootId) {
			const parentId = requireNode(this.#state, currentNodeId).parentId;
			if (parentId !== this.#state.snapshot.rootId) {
				const parentNode = requireNode(this.#state, parentId);
				const flattenedChildDirectoryId = getFlattenedChildDirectoryId(this.#state, parentId);
				if (!isDirectoryExpanded(this.#state, parentId, parentNode) && flattenedChildDirectoryId !== currentNodeId) return false;
			}
			currentNodeId = parentId;
		}
		return true;
	}
	validateChildPatch(directoryPath, patch) {
		new PathStore({
			paths: this.list(directoryPath),
			presorted: true,
			sort: this.#state.snapshot.options.sort
		}).batch(patch.operations);
	}
};
function assertOperationTargetsDirectory(directoryPath, operation) {
	switch (operation.type) {
		case "add":
		case "remove":
			if (!operation.path.startsWith(directoryPath) || operation.path === directoryPath) throw new Error(`Child patch operation must stay within ${directoryPath}: "${operation.path}"`);
			break;
		case "move":
			if (!operation.from.startsWith(directoryPath) || !operation.to.startsWith(directoryPath) || operation.from === directoryPath || operation.to === directoryPath) throw new Error(`Child patch move must stay within ${directoryPath}: "${operation.from}" -> "${operation.to}"`);
			break;
	}
}

//#endregion
export { PathStore };
//# sourceMappingURL=store.js.map