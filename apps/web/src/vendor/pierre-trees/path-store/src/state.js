import { PATH_STORE_NODE_FLAG_ROOT, getNodeDepth, hasNodeFlag, isDirectoryNode } from "./internal-types.js";

//#region ../path-store/src/state.ts
function createPathStoreState(snapshot, initialExpansion = "closed", instrumentation = null) {
	const defaultExpansion = resolveInitialExpansion(initialExpansion);
	return {
		activeNodeCount: snapshot.nodes.length - 1,
		collapsedDirectoryIds: /* @__PURE__ */ new Set(),
		collapseNewDirectoriesByDefault: false,
		defaultExpansion,
		directoriesOpenByDefault: defaultExpansion === "open",
		hasCollapsedDirectoryOverrides: false,
		directoryLoadInfoById: /* @__PURE__ */ new Map(),
		expandedDirectoryIds: /* @__PURE__ */ new Set(),
		instrumentation,
		listeners: /* @__PURE__ */ new Map(),
		pathCacheByNodeId: new Map([[snapshot.rootId, {
			path: "",
			version: 0
		}]]),
		pathCacheVersion: 0,
		snapshot,
		transactionStack: []
	};
}
function createTransactionFrame() {
	return {
		affectedAncestorIds: /* @__PURE__ */ new Set(),
		affectedNodeIds: /* @__PURE__ */ new Set(),
		events: []
	};
}
function resolveInitialExpansion(initialExpansion) {
	if (typeof initialExpansion !== "number") return initialExpansion;
	if (!Number.isInteger(initialExpansion) || initialExpansion < 0) throw new Error(`initialExpansion must be "open", "closed", or a non-negative integer depth. Received: ${String(initialExpansion)}`);
	return initialExpansion;
}
function isDirectoryExpandedByDefault(state, node) {
	if (hasNodeFlag(node, PATH_STORE_NODE_FLAG_ROOT)) return true;
	if (state.defaultExpansion === "open") return true;
	if (state.defaultExpansion === "closed") return false;
	return getNodeDepth(node) <= state.defaultExpansion;
}
function isDirectoryExpanded(state, nodeId, node = state.snapshot.nodes[nodeId]) {
	if (node == null || !isDirectoryNode(node)) return false;
	if (state.directoriesOpenByDefault && !state.hasCollapsedDirectoryOverrides) return true;
	if (state.collapsedDirectoryIds.has(nodeId)) return false;
	if (state.expandedDirectoryIds.has(nodeId)) return true;
	return isDirectoryExpandedByDefault(state, node);
}
function setDirectoryExpanded(state, nodeId, expanded, node = state.snapshot.nodes[nodeId]) {
	if (node == null || !isDirectoryNode(node)) return;
	const expandedByDefault = isDirectoryExpandedByDefault(state, node);
	if (expanded) {
		if (expandedByDefault) {
			state.collapsedDirectoryIds.delete(nodeId);
			state.hasCollapsedDirectoryOverrides = state.collapsedDirectoryIds.size > 0;
			return;
		}
		state.expandedDirectoryIds.add(nodeId);
		return;
	}
	if (expandedByDefault) {
		state.collapsedDirectoryIds.add(nodeId);
		state.hasCollapsedDirectoryOverrides = true;
		return;
	}
	state.expandedDirectoryIds.delete(nodeId);
}
function getOrCreateDirectoryLoadInfo(state, nodeId) {
	const existingInfo = state.directoryLoadInfoById.get(nodeId);
	if (existingInfo != null) return existingInfo;
	const nextInfo = {
		activeAttemptId: null,
		errorMessage: null,
		nextAttemptId: 1,
		state: "loaded"
	};
	state.directoryLoadInfoById.set(nodeId, nextInfo);
	return nextInfo;
}
function getDirectoryLoadState(state, nodeId) {
	return state.directoryLoadInfoById.get(nodeId)?.state ?? "loaded";
}
function beginDirectoryLoad(state, nodeId) {
	const loadInfo = getOrCreateDirectoryLoadInfo(state, nodeId);
	if (loadInfo.state === "loading" && loadInfo.activeAttemptId != null) return {
		attemptId: loadInfo.activeAttemptId,
		nodeId,
		reused: true
	};
	const attemptId = loadInfo.nextAttemptId;
	loadInfo.activeAttemptId = attemptId;
	loadInfo.errorMessage = null;
	loadInfo.nextAttemptId += 1;
	loadInfo.state = "loading";
	return {
		attemptId,
		nodeId,
		reused: false
	};
}
function markDirectoryUnloadedState(state, nodeId) {
	const loadInfo = getOrCreateDirectoryLoadInfo(state, nodeId);
	loadInfo.activeAttemptId = null;
	loadInfo.errorMessage = null;
	loadInfo.state = "unloaded";
}
function completeDirectoryLoad(state, nodeId, attemptId) {
	const loadInfo = state.directoryLoadInfoById.get(nodeId);
	if (loadInfo == null || loadInfo.activeAttemptId !== attemptId) return false;
	loadInfo.activeAttemptId = null;
	loadInfo.errorMessage = null;
	loadInfo.state = "loaded";
	return true;
}
function isDirectoryLoadAttemptCurrent(state, nodeId, attemptId) {
	return state.directoryLoadInfoById.get(nodeId)?.activeAttemptId === attemptId;
}
function failDirectoryLoad(state, nodeId, attemptId, errorMessage) {
	const loadInfo = state.directoryLoadInfoById.get(nodeId);
	if (loadInfo == null || loadInfo.activeAttemptId !== attemptId) return false;
	loadInfo.activeAttemptId = null;
	loadInfo.errorMessage = errorMessage ?? null;
	loadInfo.state = "error";
	return true;
}
function clearDirectoryLoadInfo(state, nodeId) {
	state.directoryLoadInfoById.delete(nodeId);
}

//#endregion
export { beginDirectoryLoad, clearDirectoryLoadInfo, completeDirectoryLoad, createPathStoreState, createTransactionFrame, failDirectoryLoad, getDirectoryLoadState, isDirectoryExpanded, isDirectoryLoadAttemptCurrent, markDirectoryUnloadedState, setDirectoryExpanded };
//# sourceMappingURL=state.js.map