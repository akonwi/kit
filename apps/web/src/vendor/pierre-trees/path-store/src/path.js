//#region ../path-store/src/path.ts
function splitCanonicalPath(inputPath) {
	const hasTrailingSlash = inputPath.length > 0 && inputPath.charCodeAt(inputPath.length - 1) === 47;
	const endIndex = hasTrailingSlash ? inputPath.length - 1 : inputPath.length;
	const segments = [];
	let segmentStart = 0;
	for (let index = 0; index < endIndex; index++) {
		if (inputPath.charCodeAt(index) !== 47) continue;
		segments.push(inputPath.slice(segmentStart, index));
		segmentStart = index + 1;
	}
	segments.push(inputPath.slice(segmentStart, endIndex));
	return {
		hasTrailingSlash,
		segments
	};
}
function parseInputPath(inputPath) {
	const { hasTrailingSlash, segments } = splitCanonicalPath(inputPath);
	return {
		basename: segments[segments.length - 1] ?? "",
		isDirectory: hasTrailingSlash,
		path: inputPath,
		segments
	};
}
function parseLookupPath(inputPath) {
	if (inputPath.length === 0) return {
		requiresDirectory: false,
		segments: []
	};
	const { hasTrailingSlash, segments } = splitCanonicalPath(inputPath);
	return {
		requiresDirectory: hasTrailingSlash,
		segments
	};
}

//#endregion
export { parseInputPath, parseLookupPath };
//# sourceMappingURL=path.js.map