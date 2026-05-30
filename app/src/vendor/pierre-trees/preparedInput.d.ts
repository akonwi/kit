import type {
	FileTreePreparedInput,
	FileTreeSortComparator,
} from "./types.js";

export declare function prepareFileTreeInput(
	paths: readonly string[],
	options?: {
		flattenEmptyDirectories?: boolean;
		sort?: "default" | FileTreeSortComparator;
	},
): FileTreePreparedInput;

export declare function preparePresortedFileTreeInput(
	paths: readonly string[],
): FileTreePreparedInput;
