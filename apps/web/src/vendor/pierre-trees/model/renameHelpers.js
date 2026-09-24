//#region src/model/renameHelpers.ts
function getRenameLeafName(path) {
	const normalizedPath = path.endsWith("/") ? path.slice(0, -1) : path;
	const separatorIndex = normalizedPath.lastIndexOf("/");
	return separatorIndex < 0 ? normalizedPath : normalizedPath.slice(separatorIndex + 1);
}
function toRenameHelperPath(path) {
	return path.endsWith("/") ? path.slice(0, -1) : path;
}
function toCanonicalRenamePath(path, isFolder) {
	return isFolder && !path.endsWith("/") ? `${path}/` : path;
}

//#endregion
export { getRenameLeafName, toCanonicalRenamePath, toRenameHelperPath };
//# sourceMappingURL=renameHelpers.js.map