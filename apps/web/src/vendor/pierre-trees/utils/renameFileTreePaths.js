import { getSelectionPath } from "./getSelectionPath.js";

//#region src/utils/renameFileTreePaths.ts
function splitPath(path) {
	const separatorIndex = path.lastIndexOf("/");
	if (separatorIndex < 0) return {
		parentPath: "",
		baseName: path
	};
	return {
		parentPath: path.slice(0, separatorIndex),
		baseName: path.slice(separatorIndex + 1)
	};
}
function joinPath(parentPath, baseName) {
	return parentPath === "" ? baseName : `${parentPath}/${baseName}`;
}
function remapExpandedPathsForFolderRename({ expandedPaths, sourcePath, destinationPath }) {
	if (expandedPaths.length === 0 || sourcePath === destinationPath) return expandedPaths;
	const sourcePrefix = `${sourcePath}/`;
	const nextExpandedPaths = [];
	const seen = /* @__PURE__ */ new Set();
	let changed = false;
	for (let index = 0; index < expandedPaths.length; index++) {
		const path = expandedPaths[index];
		const nextPath = path === sourcePath ? destinationPath : path.startsWith(sourcePrefix) ? `${destinationPath}${path.slice(sourcePath.length)}` : path;
		if (nextPath !== path) changed = true;
		if (seen.has(nextPath)) {
			changed = true;
			continue;
		}
		seen.add(nextPath);
		nextExpandedPaths.push(nextPath);
	}
	return changed ? nextExpandedPaths : expandedPaths;
}
/**
* Computes a renamed file list using same-parent basename rename semantics.
*/
function renameFileTreePaths({ files, path, isFolder, nextBasename }) {
	const sourcePath = getSelectionPath(path);
	const trimmedBasename = nextBasename.trim();
	if (trimmedBasename.length === 0) return { error: "Name cannot be empty." };
	if (trimmedBasename.includes("/")) return { error: "Name cannot include \"/\"." };
	const { parentPath, baseName } = splitPath(sourcePath);
	if (trimmedBasename === baseName) return {
		nextFiles: files,
		sourcePath,
		destinationPath: sourcePath,
		isFolder
	};
	const destinationPath = joinPath(parentPath, trimmedBasename);
	const nextFiles = new Array(files.length);
	const seenPaths = /* @__PURE__ */ new Set();
	if (!isFolder) {
		const destinationPrefix$1 = `${destinationPath}/`;
		let renamed = false;
		for (let index = 0; index < files.length; index++) {
			const file = files[index];
			if (file !== sourcePath && file.startsWith(destinationPrefix$1)) return { error: `"${destinationPath}" already exists.` };
			const nextFile = file === sourcePath ? destinationPath : file;
			if (seenPaths.has(nextFile)) return { error: `"${destinationPath}" already exists.` };
			seenPaths.add(nextFile);
			nextFiles[index] = nextFile;
			if (file === sourcePath) renamed = true;
		}
		if (!renamed) return { error: "Could not find the selected file to rename." };
		return {
			nextFiles,
			sourcePath,
			destinationPath,
			isFolder
		};
	}
	const sourcePrefix = `${sourcePath}/`;
	const destinationPrefix = `${destinationPath}/`;
	let renamedPathCount = 0;
	for (let index = 0; index < files.length; index++) {
		const file = files[index];
		const isWithinRenamedFolder = file === sourcePath || file.startsWith(sourcePrefix);
		if (!isWithinRenamedFolder && (file === destinationPath || file.startsWith(destinationPrefix))) return { error: `"${destinationPath}" already exists.` };
		const nextFile = isWithinRenamedFolder ? `${destinationPath}${file.slice(sourcePath.length)}` : file;
		if (seenPaths.has(nextFile)) return { error: `"${destinationPath}" already exists.` };
		seenPaths.add(nextFile);
		nextFiles[index] = nextFile;
		if (isWithinRenamedFolder) renamedPathCount++;
	}
	if (renamedPathCount === 0) return { error: "Could not find the selected folder to rename." };
	return {
		nextFiles,
		sourcePath,
		destinationPath,
		isFolder
	};
}

//#endregion
export { remapExpandedPathsForFolderRename, renameFileTreePaths };
//# sourceMappingURL=renameFileTreePaths.js.map