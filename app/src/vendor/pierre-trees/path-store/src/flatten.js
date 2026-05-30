import { PATH_STORE_NODE_FLAG_ROOT, hasNodeFlag, isDirectoryNode } from "./internal-types.js";

//#region ../path-store/src/flatten.ts
function getFlattenedChildDirectoryId(state, directoryNodeId) {
	if (state.snapshot.options.flattenEmptyDirectories !== true) return null;
	const directoryNode = state.snapshot.nodes[directoryNodeId];
	if (directoryNode == null || !isDirectoryNode(directoryNode) || hasNodeFlag(directoryNode, PATH_STORE_NODE_FLAG_ROOT)) return null;
	const directoryIndex = state.snapshot.directories.get(directoryNodeId);
	if (directoryIndex == null || directoryIndex.childIds.length !== 1) return null;
	const childId = directoryIndex.childIds[0];
	if (childId == null) return null;
	const childNode = state.snapshot.nodes[childId];
	if (childNode == null || !isDirectoryNode(childNode)) return null;
	return childId;
}
function getFlattenedTerminalDirectoryId(state, directoryNodeId) {
	let currentDirectoryId = directoryNodeId;
	while (true) {
		const nextDirectoryId = getFlattenedChildDirectoryId(state, currentDirectoryId);
		if (nextDirectoryId == null) return currentDirectoryId;
		currentDirectoryId = nextDirectoryId;
	}
}
function collectFlattenedDirectoryChainIds(state, directoryNodeId) {
	const chainIds = [directoryNodeId];
	let currentDirectoryId = directoryNodeId;
	while (true) {
		const nextDirectoryId = getFlattenedChildDirectoryId(state, currentDirectoryId);
		if (nextDirectoryId == null) return chainIds;
		chainIds.push(nextDirectoryId);
		currentDirectoryId = nextDirectoryId;
	}
}

//#endregion
export { collectFlattenedDirectoryChainIds, getFlattenedChildDirectoryId, getFlattenedTerminalDirectoryId };
//# sourceMappingURL=flatten.js.map