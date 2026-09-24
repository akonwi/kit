//#region src/model/pathHelpers.ts
function arePathSetsEqual(currentPaths, nextPaths) {
	if (currentPaths.size !== nextPaths.length) return false;
	for (const path of nextPaths) if (!currentPaths.has(path)) return false;
	return true;
}
function getAncestorDirectoryPaths(path) {
	const normalizedPath = path.endsWith("/") ? path.slice(0, -1) : path;
	if (normalizedPath.length === 0) return [];
	const segments = normalizedPath.split("/");
	return segments.slice(0, -1).map((_, index) => `${segments.slice(0, index + 1).join("/")}/`);
}
function getImmediateParentPath(path) {
	return getAncestorDirectoryPaths(path).at(-1) ?? null;
}
function getSiblingComparisonKey(path, parentPath) {
	if (parentPath == null) return path;
	return path.startsWith(parentPath) ? path.slice(parentPath.length) : path;
}
function isCanonicalDirectoryPath(path) {
	return path.endsWith("/");
}
const toLowerCaseSearchPath = (path) => path.toLowerCase();

//#endregion
export { arePathSetsEqual, getAncestorDirectoryPaths, getImmediateParentPath, getSiblingComparisonKey, isCanonicalDirectoryPath, toLowerCaseSearchPath };
//# sourceMappingURL=pathHelpers.js.map