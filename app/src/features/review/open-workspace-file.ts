export function resolveReviewWorkspaceFilePath(options: {
	mode: "tree" | "patch";
	focusedPath: string | null;
	changedFilePaths: ReadonlySet<string>;
	selectedPath: string | null;
}): string | null {
	if (options.mode === "tree" && options.focusedPath) {
		return options.changedFilePaths.has(options.focusedPath)
			? options.focusedPath
			: null;
	}
	return options.selectedPath;
}
