import { FLATTENED_PREFIX } from "../constants.js";

//#region src/utils/getSelectionPath.ts
const getSelectionPath = (path) => path.startsWith(FLATTENED_PREFIX) ? path.slice(FLATTENED_PREFIX.length) : path;

//#endregion
export { getSelectionPath };
//# sourceMappingURL=getSelectionPath.js.map