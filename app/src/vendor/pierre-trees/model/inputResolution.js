import { PathStore } from "../path-store/src/store.js";
import "../path-store/src/index.js";

//#region src/model/inputResolution.ts
function haveMatchingPaths(currentPaths, preparedPaths) {
	if (currentPaths === preparedPaths) return true;
	if (currentPaths.length !== preparedPaths.length) return false;
	for (let index = 0; index < currentPaths.length; index += 1) if (currentPaths[index] !== preparedPaths[index]) return false;
	return true;
}
function resolveFileTreeInput(options, context, sort) {
	const { paths, preparedInput } = options;
	if (preparedInput == null) {
		if (paths == null) throw new Error("FileTree requires paths or preparedInput");
		return {
			paths,
			preparedInput: void 0
		};
	}
	const preparedPaths = preparedInput.paths;
	if (paths == null) return {
		paths: preparedPaths,
		preparedInput
	};
	if (!haveMatchingPaths(PathStore.preparePaths(paths, sort == null ? {} : { sort }), preparedPaths)) throw new Error(`FileTree ${context} received paths and preparedInput for different path lists`);
	return {
		paths: preparedPaths,
		preparedInput
	};
}

//#endregion
export { resolveFileTreeInput };
//# sourceMappingURL=inputResolution.js.map