//#region ../path-store/src/internal-types.ts
const PATH_STORE_NODE_KIND_FILE = 0;
const PATH_STORE_NODE_KIND_DIRECTORY = 1;
const PATH_STORE_NODE_FLAG_EXPLICIT = 1;
const PATH_STORE_NODE_FLAG_ROOT = 2;
const PATH_STORE_NODE_FLAG_REMOVED = 4;
const PATH_STORE_NODE_FLAGS_MASK = PATH_STORE_NODE_FLAG_ROOT | 5;
const PATH_STORE_NODE_KIND_SHIFT = 3;
const PATH_STORE_NODE_KIND_MASK = 1 << PATH_STORE_NODE_KIND_SHIFT;
const PATH_STORE_NODE_DEPTH_SHIFT = 4;
function createNodeDepthAndFlags(depth, flags, kind = PATH_STORE_NODE_KIND_FILE) {
	return depth << PATH_STORE_NODE_DEPTH_SHIFT | kind << PATH_STORE_NODE_KIND_SHIFT | flags;
}
function getNodeDepth(node) {
	return node.depthAndFlags >>> PATH_STORE_NODE_DEPTH_SHIFT;
}
function getNodeKind(node) {
	return (node.depthAndFlags & PATH_STORE_NODE_KIND_MASK) >> PATH_STORE_NODE_KIND_SHIFT;
}
function isDirectoryNode(node) {
	return (node.depthAndFlags & PATH_STORE_NODE_KIND_MASK) !== 0;
}
function getNodeFlags(node) {
	return node.depthAndFlags & PATH_STORE_NODE_FLAGS_MASK;
}
function hasNodeFlag(node, flag) {
	return (getNodeFlags(node) & flag) !== 0;
}
function addNodeFlag(node, flag) {
	node.depthAndFlags |= flag;
}
function setNodeDepth(node, depth) {
	node.depthAndFlags = createNodeDepthAndFlags(depth, getNodeFlags(node), getNodeKind(node));
}

//#endregion
export { PATH_STORE_NODE_FLAG_EXPLICIT, PATH_STORE_NODE_FLAG_REMOVED, PATH_STORE_NODE_FLAG_ROOT, PATH_STORE_NODE_KIND_DIRECTORY, PATH_STORE_NODE_KIND_FILE, addNodeFlag, createNodeDepthAndFlags, getNodeDepth, getNodeKind, hasNodeFlag, isDirectoryNode, setNodeDepth };
//# sourceMappingURL=internal-types.js.map