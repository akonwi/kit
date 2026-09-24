import { PathStore } from "./path-store/src/store.js";
import "./path-store/src/index.js";

//#region src/preparedInput.ts
function prepareFileTreeInput(paths, options = {}) {
	return PathStore.prepareInput(paths, options);
}
function preparePresortedFileTreeInput(paths) {
	return PathStore.preparePresortedInput(paths);
}

//#endregion
export { prepareFileTreeInput, preparePresortedFileTreeInput };
//# sourceMappingURL=preparedInput.js.map