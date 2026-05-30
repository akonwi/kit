//#region src/model/dragAndDrop.ts
function isCanonicalDirectoryPath(path) {
	return path.endsWith("/");
}
function getPathBasename(path) {
	const trimmedPath = path.endsWith("/") ? path.slice(0, -1) : path;
	const lastSlashIndex = trimmedPath.lastIndexOf("/");
	const basename = lastSlashIndex < 0 ? trimmedPath : trimmedPath.slice(lastSlashIndex + 1);
	return path.endsWith("/") ? `${basename}/` : basename;
}
function normalizeDraggedPaths(paths) {
	const uniquePaths = [];
	const seenPaths = /* @__PURE__ */ new Set();
	for (const path of paths) {
		if (seenPaths.has(path)) continue;
		seenPaths.add(path);
		uniquePaths.push(path);
	}
	const keptPaths = /* @__PURE__ */ new Set();
	for (const path of uniquePaths.toSorted((left, right) => {
		if (left.length !== right.length) return left.length - right.length;
		return left.localeCompare(right);
	})) {
		const segments = (path.endsWith("/") ? path.slice(0, -1) : path).split("/");
		let hasSelectedAncestor = false;
		for (let index = 0; index < segments.length - 1; index += 1) {
			const ancestorPath = `${segments.slice(0, index + 1).join("/")}/`;
			if (!keptPaths.has(ancestorPath)) continue;
			hasSelectedAncestor = true;
			break;
		}
		if (hasSelectedAncestor) continue;
		keptPaths.add(path);
	}
	return uniquePaths.filter((path) => keptPaths.has(path));
}
function resolveDraggedPathsForStart(path, selectedPaths) {
	return selectedPaths.includes(path) ? normalizeDraggedPaths(selectedPaths) : [path];
}
function dropTargetsEqual(left, right) {
	if (left === right) return true;
	if (left == null || right == null) return false;
	return left.kind === right.kind && left.directoryPath === right.directoryPath && left.flattenedSegmentPath === right.flattenedSegmentPath && left.hoveredPath === right.hoveredPath;
}
function createDropContext(draggedPaths, target) {
	return {
		draggedPaths,
		target
	};
}
function isSelfOrDescendantDrop(draggedPaths, target) {
	if (target.kind !== "directory" || target.directoryPath == null) return false;
	for (const draggedPath of draggedPaths) {
		if (!isCanonicalDirectoryPath(draggedPath)) continue;
		if (target.directoryPath === draggedPath || target.directoryPath.startsWith(draggedPath)) return true;
	}
	return false;
}
function resolveMoveDestinationPath(sourcePath, target) {
	if (target.kind === "root" || target.directoryPath == null) return getPathBasename(sourcePath);
	return target.directoryPath;
}
function buildDropOperations(draggedPaths, target) {
	const operations = draggedPaths.map((draggedPath) => {
		const destinationPath = resolveMoveDestinationPath(draggedPath, target);
		if (destinationPath === draggedPath) return null;
		return {
			from: draggedPath,
			to: destinationPath,
			type: "move"
		};
	}).filter((operation) => {
		return operation != null;
	});
	if (operations.length === 0) return null;
	return {
		operations,
		result: {
			draggedPaths,
			operation: operations.length === 1 ? "move" : "batch",
			target
		}
	};
}

//#endregion
export { buildDropOperations, createDropContext, dropTargetsEqual, isSelfOrDescendantDrop, normalizeDraggedPaths, resolveDraggedPathsForStart };
//# sourceMappingURL=dragAndDrop.js.map