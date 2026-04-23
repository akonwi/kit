import { spawnSync } from "node:child_process";
import { parsePatchFiles } from "@pierre/diffs";

function getGitDiff(): string {
	const result = spawnSync(
		"git",
		[
			"diff",
			"--cached",
			"--no-ext-diff",
			"--find-renames",
			"--find-copies",
			"--unified=3",
		],
		{ encoding: "utf8" },
	);
	if (result.status !== 0) {
		throw new Error(result.stderr || "Failed to read staged diff.");
	}

	const staged = result.stdout;
	const unstagedResult = spawnSync(
		"git",
		["diff", "--no-ext-diff", "--find-renames", "--find-copies", "--unified=3"],
		{ encoding: "utf8" },
	);
	if (unstagedResult.status !== 0) {
		throw new Error(unstagedResult.stderr || "Failed to read unstaged diff.");
	}

	const unstaged = unstagedResult.stdout;
	return [staged, unstaged]
		.filter((value) => value.trim().length > 0)
		.join("\n");
}

function main() {
	const diffText = getGitDiff();
	if (!diffText.trim()) {
		console.log("No staged or unstaged diff to parse.");
		return;
	}

	const parsed = parsePatchFiles(diffText, "review-spike");
	console.log(`Parsed patch groups: ${parsed.length}`);
	for (const [patchIndex, patch] of parsed.entries()) {
		console.log(`\nPatch ${patchIndex + 1}: ${patch.files.length} files`);
		for (const file of patch.files) {
			console.log(
				`- ${file.type} ${file.name}${file.prevName ? ` (from ${file.prevName})` : ""}`,
			);
			console.log(
				`  hunks=${file.hunks.length} partial=${file.isPartial} splitLines=${file.splitLineCount} unifiedLines=${file.unifiedLineCount}`,
			);
			for (const [hunkIndex, hunk] of file.hunks.entries()) {
				console.log(
					`    hunk ${hunkIndex + 1}: ${hunk.hunkSpecs ?? "(no header)"}${hunk.hunkContext ? ` ${hunk.hunkContext}` : ""}`,
				);
			}
		}
	}
}

main();
