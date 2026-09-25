//#region src/constants.ts
const FILE_TREE_TAG_NAME = "file-tree-container";
const FILE_TREE_STYLE_ATTRIBUTE = "data-file-tree-style";
const FILE_TREE_UNSAFE_CSS_ATTRIBUTE = "data-file-tree-unsafe-css";
const FILE_TREE_SCROLLBAR_MEASURE_ATTRIBUTE = "data-file-tree-scrollbar-measure";
const FILE_TREE_SCROLLBAR_GUTTER_STYLE_ATTRIBUTE = "data-file-tree-scrollbar-gutter-measured";
const FILE_TREE_SCROLLBAR_GUTTER_MEASURED_PROPERTY = "--trees-scrollbar-gutter-measured";
/**
* Prefix used for flattened node IDs.
* Flattened nodes represent collapsed chains of single-child folders.
* Example: 'f::src/utils/deep' represents the chain src → utils → deep
*/
const FLATTENED_PREFIX = "f::";
const HEADER_SLOT_NAME = "header";
const CONTEXT_MENU_SLOT_NAME = "context-menu";
const CONTEXT_MENU_TRIGGER_TYPE = "context-menu-trigger";

//#endregion
export { CONTEXT_MENU_SLOT_NAME, CONTEXT_MENU_TRIGGER_TYPE, FILE_TREE_SCROLLBAR_GUTTER_MEASURED_PROPERTY, FILE_TREE_SCROLLBAR_GUTTER_STYLE_ATTRIBUTE, FILE_TREE_SCROLLBAR_MEASURE_ATTRIBUTE, FILE_TREE_STYLE_ATTRIBUTE, FILE_TREE_TAG_NAME, FILE_TREE_UNSAFE_CSS_ATTRIBUTE, FLATTENED_PREFIX, HEADER_SLOT_NAME };
//# sourceMappingURL=constants.js.map