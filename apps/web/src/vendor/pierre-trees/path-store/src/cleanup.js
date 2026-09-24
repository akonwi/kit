import { rebuildDirectoryChildAggregates } from "./child-index.js";
import { PATH_STORE_NODE_FLAG_REMOVED, PATH_STORE_NODE_FLAG_ROOT, hasNodeFlag, isDirectoryNode } from "./internal-types.js";
import { attachBenchmarkInstrumentation, withBenchmarkPhase } from "./internal/benchmarkInstrumentation.js";
import { createSegmentTable, getSegmentValue, internSegment } from "./segments.js";
import { PathStoreBuilder } from "./builder.js";
import { getDirectoryLoadState, setDirectoryExpanded } from "./state.js";
import { findNodeId, listPaths, materializeNodePath, recomputeCountsRecursive, requireNode } from "./canonical.js";

//#region ../path-store/src/cleanup.ts
function isLiveNode(node) {
	return node != null && !hasNodeFlag(node, PATH_STORE_NODE_FLAG_REMOVED);
}
function isLiveDirectoryNode(state, nodeId) {
	const node = state.snapshot.nodes[nodeId];
	if (!isLiveNode(node) || !isDirectoryNode(node) || hasNodeFlag(node, PATH_STORE_NODE_FLAG_ROOT)) return null;
	return node;
}
function countCachedPathEntries(state) {
	let cachedPathEntryCount = 0;
	for (const [nodeId, cachedEntry] of state.pathCacheByNodeId) {
		if (cachedEntry.version !== state.pathCacheVersion) continue;
		if (!isLiveNode(state.snapshot.nodes[nodeId])) continue;
		cachedPathEntryCount += 1;
	}
	return cachedPathEntryCount;
}
function countSegmentEntries(segmentTable) {
	return Math.max(0, segmentTable.valueById.length - 1);
}
function createCleanupMetricSnapshot(state) {
	return {
		activeNodeCount: state.activeNodeCount,
		cachedPathEntryCount: countCachedPathEntries(state),
		loadInfoEntryCount: state.directoryLoadInfoById.size,
		segmentCount: countSegmentEntries(state.snapshot.segmentTable),
		totalNodeSlotCount: Math.max(0, state.snapshot.nodes.length - 1)
	};
}
function createCleanupResult(mode, idsPreserved, before, after) {
	return {
		activeNodeCountAfter: after.activeNodeCount,
		activeNodeCountBefore: before.activeNodeCount,
		cachedPathEntryCountAfter: after.cachedPathEntryCount,
		cachedPathEntryCountBefore: before.cachedPathEntryCount,
		idsPreserved,
		loadInfoEntryCountAfter: after.loadInfoEntryCount,
		loadInfoEntryCountBefore: before.loadInfoEntryCount,
		mode,
		reclaimedCachedPathEntryCount: before.cachedPathEntryCount - after.cachedPathEntryCount,
		reclaimedLoadInfoEntryCount: before.loadInfoEntryCount - after.loadInfoEntryCount,
		reclaimedNodeSlotCount: before.totalNodeSlotCount - after.totalNodeSlotCount,
		reclaimedSegmentCount: before.segmentCount - after.segmentCount,
		segmentCountAfter: after.segmentCount,
		segmentCountBefore: before.segmentCount,
		totalNodeSlotCountAfter: after.totalNodeSlotCount,
		totalNodeSlotCountBefore: before.totalNodeSlotCount
	};
}
function collectExpansionOverridePaths(state) {
	const collapsedPaths = [];
	const expandedPaths = [];
	for (const nodeId of state.collapsedDirectoryIds) if (isLiveDirectoryNode(state, nodeId) != null) collapsedPaths.push(materializeNodePath(state, nodeId));
	for (const nodeId of state.expandedDirectoryIds) if (isLiveDirectoryNode(state, nodeId) != null) expandedPaths.push(materializeNodePath(state, nodeId));
	return {
		collapsedPaths,
		expandedPaths
	};
}
function collectDirectoryLoadInfos(state) {
	const retainedInfos = [];
	for (const [nodeId, info] of state.directoryLoadInfoById) {
		if (isLiveDirectoryNode(state, nodeId) == null || getDirectoryLoadState(state, nodeId) === "loaded") continue;
		retainedInfos.push({
			info: {
				activeAttemptId: null,
				errorMessage: info.errorMessage,
				nextAttemptId: info.nextAttemptId,
				state: info.state
			},
			path: materializeNodePath(state, nodeId)
		});
	}
	return retainedInfos;
}
function restoreExpansionOverridePaths(state, persistedExpansionState) {
	state.collapsedDirectoryIds.clear();
	state.hasCollapsedDirectoryOverrides = false;
	state.expandedDirectoryIds.clear();
	for (const path of persistedExpansionState.expandedPaths) {
		const nodeId = findNodeId(state, path);
		if (nodeId == null) continue;
		setDirectoryExpanded(state, nodeId, true, requireNode(state, nodeId));
	}
	for (const path of persistedExpansionState.collapsedPaths) {
		const nodeId = findNodeId(state, path);
		if (nodeId == null) continue;
		setDirectoryExpanded(state, nodeId, false, requireNode(state, nodeId));
	}
}
function restoreDirectoryLoadInfos(state, persistedLoadInfos) {
	state.directoryLoadInfoById.clear();
	for (const retainedInfo of persistedLoadInfos) {
		const nodeId = findNodeId(state, retainedInfo.path);
		if (nodeId == null) continue;
		if (isLiveDirectoryNode(state, nodeId) == null) continue;
		state.directoryLoadInfoById.set(nodeId, {
			activeAttemptId: null,
			errorMessage: retainedInfo.info.errorMessage,
			nextAttemptId: retainedInfo.info.nextAttemptId,
			state: retainedInfo.info.state
		});
	}
}
function clearPathCaches(state) {
	state.pathCacheVersion += 1;
	state.pathCacheByNodeId.clear();
	state.pathCacheByNodeId.set(state.snapshot.rootId, {
		path: "",
		version: state.pathCacheVersion
	});
}
function rebuildSegmentTablePreservingNodeIds(state) {
	const previousSegmentTable = state.snapshot.segmentTable;
	const nextSegmentTable = createSegmentTable();
	for (const node of state.snapshot.nodes) {
		if (!isLiveNode(node)) continue;
		if (hasNodeFlag(node, PATH_STORE_NODE_FLAG_ROOT)) {
			node.nameId = 0;
			continue;
		}
		node.nameId = internSegment(nextSegmentTable, getSegmentValue(previousSegmentTable, node.nameId));
	}
	state.snapshot.segmentTable = nextSegmentTable;
}
function rebuildDirectoryIndexes(state) {
	for (const [directoryId, directoryIndex] of state.snapshot.directories) {
		const directoryNode = state.snapshot.nodes[directoryId];
		if (!isLiveNode(directoryNode) || !isDirectoryNode(directoryNode)) {
			state.snapshot.directories.delete(directoryId);
			continue;
		}
		const liveChildIds = directoryIndex.childIds.filter((childId) => {
			const childNode = state.snapshot.nodes[childId];
			return isLiveNode(childNode) && childNode.parentId === directoryId;
		});
		directoryIndex.childIds = liveChildIds;
		directoryIndex.childIdByNameId = new Map(liveChildIds.map((childId) => [requireNode(state, childId).nameId, childId]));
		directoryIndex.childPositionById = new Map(liveChildIds.map((childId, childIndex) => [childId, childIndex]));
		rebuildDirectoryChildAggregates(state.snapshot.nodes, directoryIndex);
	}
}
function trimTrailingRemovedNodeSlots(state) {
	let lastNodeIndex = state.snapshot.nodes.length - 1;
	while (lastNodeIndex > state.snapshot.rootId) {
		const node = state.snapshot.nodes[lastNodeIndex];
		if (isLiveNode(node)) break;
		lastNodeIndex -= 1;
	}
	state.snapshot.nodes.length = lastNodeIndex + 1;
}
function runStableCleanup(state) {
	const persistedExpansionState = collectExpansionOverridePaths(state);
	const persistedLoadInfos = collectDirectoryLoadInfos(state);
	withBenchmarkPhase(state.instrumentation, "store.cleanup.stable.clearPathCaches", () => clearPathCaches(state));
	withBenchmarkPhase(state.instrumentation, "store.cleanup.stable.rebuildSegmentTable", () => rebuildSegmentTablePreservingNodeIds(state));
	withBenchmarkPhase(state.instrumentation, "store.cleanup.stable.rebuildDirectoryIndexes", () => rebuildDirectoryIndexes(state));
	withBenchmarkPhase(state.instrumentation, "store.cleanup.stable.trimTrailingRemovedNodeSlots", () => trimTrailingRemovedNodeSlots(state));
	withBenchmarkPhase(state.instrumentation, "store.cleanup.stable.restoreExpansionOverrides", () => restoreExpansionOverridePaths(state, persistedExpansionState));
	withBenchmarkPhase(state.instrumentation, "store.cleanup.stable.restoreDirectoryLoadInfos", () => restoreDirectoryLoadInfos(state, persistedLoadInfos));
	withBenchmarkPhase(state.instrumentation, "store.cleanup.stable.recomputeCounts", () => recomputeCountsRecursive(state, state.snapshot.rootId));
}
function runAggressiveCleanup(state) {
	const persistedExpansionState = collectExpansionOverridePaths(state);
	const persistedLoadInfos = collectDirectoryLoadInfos(state);
	const canonicalPaths = withBenchmarkPhase(state.instrumentation, "store.cleanup.aggressive.listPaths", () => listPaths(state));
	const builderOptions = attachBenchmarkInstrumentation({ ...state.snapshot.options }, state.instrumentation);
	const rebuiltSnapshot = withBenchmarkPhase(state.instrumentation, "store.cleanup.aggressive.rebuildSnapshot", () => {
		const builder = new PathStoreBuilder(builderOptions);
		builder.appendPaths(canonicalPaths);
		return builder.finish();
	});
	state.snapshot = rebuiltSnapshot;
	state.activeNodeCount = rebuiltSnapshot.nodes.length - 1;
	state.pathCacheByNodeId = new Map([[rebuiltSnapshot.rootId, {
		path: "",
		version: 0
	}]]);
	state.pathCacheVersion = 0;
	withBenchmarkPhase(state.instrumentation, "store.cleanup.aggressive.restoreExpansionOverrides", () => restoreExpansionOverridePaths(state, persistedExpansionState));
	withBenchmarkPhase(state.instrumentation, "store.cleanup.aggressive.restoreDirectoryLoadInfos", () => restoreDirectoryLoadInfos(state, persistedLoadInfos));
	withBenchmarkPhase(state.instrumentation, "store.cleanup.aggressive.recomputeCounts", () => recomputeCountsRecursive(state, state.snapshot.rootId));
}
function hasActiveCleanupBlockingLoads(state) {
	for (const loadInfo of state.directoryLoadInfoById.values()) if (loadInfo.state === "loading" && loadInfo.activeAttemptId != null) return true;
	return false;
}
function cleanupPathStoreState(state, mode) {
	const before = createCleanupMetricSnapshot(state);
	if (mode === "stable") withBenchmarkPhase(state.instrumentation, "store.cleanup.stable", () => runStableCleanup(state));
	else withBenchmarkPhase(state.instrumentation, "store.cleanup.aggressive", () => runAggressiveCleanup(state));
	const after = createCleanupMetricSnapshot(state);
	return createCleanupResult(mode, mode === "stable", before, after);
}

//#endregion
export { cleanupPathStoreState, hasActiveCleanupBlockingLoads };
//# sourceMappingURL=cleanup.js.map