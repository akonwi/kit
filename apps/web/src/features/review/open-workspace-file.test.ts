import { describe, expect, test } from "bun:test";
import { resolveReviewWorkspaceFilePath } from "./open-workspace-file";

const changedFilePaths = new Set(["src/a.ts", "src/b.ts"]);

describe("review workspace file opening", () => {
	test("opens the focused changed file from the tree", () => {
		expect(
			resolveReviewWorkspaceFilePath({
				mode: "tree",
				focusedPath: "src/a.ts",
				changedFilePaths,
				selectedPath: "src/b.ts",
			}),
		).toBe("src/a.ts");
	});

	test("does not fall back to a stale file when a directory is focused", () => {
		expect(
			resolveReviewWorkspaceFilePath({
				mode: "tree",
				focusedPath: "src",
				changedFilePaths,
				selectedPath: "src/b.ts",
			}),
		).toBeNull();
	});

	test("opens the selected file from the patch", () => {
		expect(
			resolveReviewWorkspaceFilePath({
				mode: "patch",
				focusedPath: "src",
				changedFilePaths,
				selectedPath: "src/b.ts",
			}),
		).toBe("src/b.ts");
	});
});
