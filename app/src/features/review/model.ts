import { spawnSync } from "node:child_process";
import { type Dirent, existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import type { FileDiffMetadata, Hunk as PierreHunk } from "@pierre/diffs";
import { parsePatchFiles } from "@pierre/diffs";
import { safeProcessCwd } from "../../process-cwd";
import type { ReviewHunk, ReviewLine } from "../../shell/diff/types";
import { inferFiletype } from "../../shell/filetype";

export type { ReviewHunk, ReviewLine } from "../../shell/diff/types";

export type ReviewDiffSource = "working" | "untracked" | "commit";

/**
 * What the review screen is diffing.
 *
 * - `working` — working tree vs HEAD plus untracked files (default).
 * - `commit` — a single commit vs its (first) parent.
 * - `branch` — the branch's total diff: merge-base(base, head)..head.
 *   `head` is pinned at selection time so the diff is deterministic even
 *   if the branch moves while the review is open.
 *
 * Deliberately not a history explorer: no arbitrary ranges or graphs.
 * See docs/features/code-review-commit-targets.md.
 */
export type ReviewTarget =
	| { kind: "working" }
	| { kind: "commit"; sha: string }
	| { kind: "branch"; base: string; head: string; mergeBase: string };

export type ReviewCommitSummary = {
	/** Full sha — canonical identifier for targets, keys, and submissions. */
	sha: string;
	/** Abbreviated sha for display only. */
	shortSha: string;
	subject: string;
	relativeTime: string;
};

export type ReviewSkippedSection = {
	id: string;
	beforeHunkIndex: number;
	rawPatch: string;
	lineCount: number;
	additionStart: number;
	deletionStart: number;
};

export type ReviewFile = {
	id: string;
	noteKey: string;
	path: string;
	prevPath?: string;
	status: FileDiffMetadata["type"];
	source: ReviewDiffSource;
	filetype?: string;
	rawPatch: string;
	hunks: ReviewHunk[];
	skippedSections: ReviewSkippedSection[];
	changeCount: number;
	unifiedLineCount: number;
	splitLineCount: number;
};

function runGit(
	cwd: string | undefined,
	args: string[],
	errorMessage: string,
): string {
	const result = spawnSync("git", args, {
		encoding: "utf8",
		cwd: cwd || safeProcessCwd(),
	});
	if (result.status !== 0) {
		throw new Error(result.stderr || errorMessage);
	}
	return result.stdout;
}

function tryRunGit(cwd: string | undefined, args: string[]): string | null {
	const result = spawnSync("git", args, {
		encoding: "utf8",
		cwd: cwd || safeProcessCwd(),
	});
	if (result.status !== 0) return null;
	return result.stdout;
}

/** True when the git command exits 0; for status-only checks. */
function gitSucceeds(cwd: string | undefined, args: string[]): boolean {
	const result = spawnSync("git", args, {
		encoding: "utf8",
		cwd: cwd || safeProcessCwd(),
	});
	return result.status === 0;
}

function getGitRepoRoot(cwd?: string): string | null {
	return tryRunGit(cwd, ["rev-parse", "--show-toplevel"])?.trim() || null;
}

/**
 * Detect whether the current branch has a HEAD commit. An unborn branch
 * (e.g. immediately after `git init`) has no HEAD; `git diff HEAD` would
 * fail there.
 */
function repoHasHead(cwd?: string): boolean {
	const result = spawnSync(
		"git",
		["rev-parse", "--verify", "--quiet", "HEAD"],
		{
			encoding: "utf8",
			cwd: cwd || safeProcessCwd(),
		},
	);
	return result.status === 0;
}

/**
 * Diff from HEAD to working tree — covers staged and unstaged changes in a
 * single patch. The review intentionally does not distinguish between the
 * two; users just want to see what they've changed since HEAD. A side
 * benefit is that each path appears at most once, so the file tree can't
 * receive duplicate entries for the same path.
 *
 * On an unborn branch (no HEAD) we fall back to `git diff --cached`, which
 * surfaces staged files against the empty tree. Working-tree-only changes
 * to staged files are not shown in that case, but unborn-branch usage is
 * rare enough that this trade-off is acceptable.
 */
function getWorkingTreeDiff(cwd?: string): string {
	const baseArgs = [
		"--no-ext-diff",
		"--find-renames",
		"--find-copies",
		"--unified=3",
	];
	if (!repoHasHead(cwd)) {
		return runGit(
			cwd,
			["diff", "--cached", ...baseArgs],
			"Failed to read staged diff.",
		);
	}
	return runGit(
		cwd,
		["diff", "HEAD", ...baseArgs],
		"Failed to read working tree diff.",
	);
}

/** SHA of git's canonical empty tree; parent of a root commit's diff. */
const EMPTY_TREE_SHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904";

/**
 * Diff between two revisions. Returns empty on failure (e.g. a pruned
 * or ambiguous sha) instead of throwing — a resource error would
 * propagate through every downstream memo and crash the review overlay.
 */
function getRevisionDiff(
	cwd: string | undefined,
	before: string,
	after: string,
): string {
	return (
		tryRunGit(cwd, [
			"diff",
			before,
			after,
			"--no-ext-diff",
			"--find-renames",
			"--find-copies",
			"--unified=3",
			"--",
		]) ?? ""
	);
}

/** Diff endpoints for a committed target. */
export function revisionsForTarget(
	cwd: string | undefined,
	target: ReviewTarget,
): ReviewRevisions | null {
	switch (target.kind) {
		case "working":
			return null;
		case "commit": {
			const parent =
				tryRunGit(cwd, [
					"rev-parse",
					"--verify",
					"--quiet",
					`${target.sha}^`,
				])?.trim() || EMPTY_TREE_SHA;
			return {
				after: target.sha,
				before: parent,
				key: `commit:${target.sha}`,
			};
		}
		case "branch":
			return {
				after: target.head,
				before: target.mergeBase,
				key: `branch:${target.base}:${target.mergeBase}:${target.head}`,
			};
	}
}

const COMMIT_SUMMARY_FORMAT = "--format=%H%x00%h%x00%s%x00%cr";

function parseCommitSummaryLine(line: string): ReviewCommitSummary | null {
	const [sha, shortSha, subject, relativeTime] = line.split("\0");
	if (!sha) return null;
	return {
		sha,
		shortSha: shortSha || sha.slice(0, 7),
		subject: subject ?? "",
		relativeTime: relativeTime ?? "",
	};
}

/** Most recent commits, newest first. Capped by design; see the doc. */
export function listRecentCommits(
	cwd?: string,
	limit = 20,
): ReviewCommitSummary[] {
	const output = tryRunGit(cwd, ["log", `-${limit}`, COMMIT_SUMMARY_FORMAT]);
	if (!output) return [];
	return output
		.split("\n")
		.filter(Boolean)
		.map(parseCommitSummaryLine)
		.filter((commit): commit is ReviewCommitSummary => commit !== null);
}

/**
 * True when `sha` is still part of HEAD's history. An amended or
 * rebased-away commit stops being an ancestor of HEAD even though the
 * old object survives in the reflog — this is the staleness signal for
 * reviews drafted against a pinned commit.
 */
export function isAncestorOfHead(
	cwd: string | undefined,
	sha: string,
): boolean {
	return gitSucceeds(cwd, ["merge-base", "--is-ancestor", sha, "HEAD"]);
}

/** Full sha of a commit's first parent; empty-tree sha for root commits. */
export function resolveCommitParent(
	cwd: string | undefined,
	sha: string,
): string {
	return (
		tryRunGit(cwd, ["rev-parse", "--verify", "--quiet", `${sha}^`])?.trim() ||
		EMPTY_TREE_SHA
	);
}

/** Current branch name, or null when detached/unavailable. */
export function getCurrentBranch(cwd?: string): string | null {
	const name = tryRunGit(cwd, ["rev-parse", "--abbrev-ref", "HEAD"])?.trim();
	if (!name || name === "HEAD") return null;
	return name;
}

/** Full sha of the merge base of two refs, or null when unrelated. */
export function getMergeBase(
	cwd: string | undefined,
	a: string,
	b: string,
): string | null {
	return tryRunGit(cwd, ["merge-base", a, b])?.trim() || null;
}

/**
 * Default base for branch diffs: the local default branch (`main`, then
 * `master`, then whatever local branch the remote's default points at),
 * falling back to the remote-tracking ref only when no local branch
 * exists. Local-first keeps the diff anchored to what's checked out on
 * this machine rather than the remote's state. Returns null when nothing
 * resolves or the base is the current commit (nothing to diff).
 */
export function resolveDefaultBranchBase(cwd?: string): string | null {
	const originHead = tryRunGit(cwd, [
		"symbolic-ref",
		"--quiet",
		"refs/remotes/origin/HEAD",
	])
		?.trim()
		.replace("refs/remotes/", "");
	// The local branch name the remote default points at (origin/main -> main).
	const originHeadLocal = originHead?.replace(/^origin\//, "");
	const seen = new Set<string>();
	const candidates = [originHeadLocal, "main", "master", originHead].filter(
		(value): value is string => {
			if (!value || seen.has(value)) return false;
			seen.add(value);
			return true;
		},
	);
	const headSha = tryRunGit(cwd, ["rev-parse", "HEAD"])?.trim();
	for (const candidate of candidates) {
		const sha = tryRunGit(cwd, [
			"rev-parse",
			"--verify",
			"--quiet",
			`${candidate}^{commit}`,
		])?.trim();
		if (!sha) continue;
		if (headSha && sha === headSha) continue;
		return candidate;
	}
	return null;
}

export type ReviewBranchSummary = {
	name: string;
	relativeTime: string;
};

/**
 * Local branches by recency of their last commit, excluding the current
 * branch. Candidates for the branch-diff base.
 */
export function listLocalBranches(cwd?: string): ReviewBranchSummary[] {
	const current = getCurrentBranch(cwd);
	const output = tryRunGit(cwd, [
		"for-each-ref",
		"refs/heads",
		"--sort=-committerdate",
		"--format=%(refname:short)%00%(committerdate:relative)",
	]);
	if (!output) return [];
	return output
		.split("\n")
		.filter(Boolean)
		.map((line) => {
			const [name, relativeTime] = line.split("\0");
			return { name: name ?? "", relativeTime: relativeTime ?? "" };
		})
		.filter((branch) => branch.name.length > 0 && branch.name !== current);
}

/** Resolve a ref to its commit summary, or null when it doesn't exist. */
export function resolveCommit(
	cwd: string | undefined,
	ref: string,
): ReviewCommitSummary | null {
	const output = tryRunGit(cwd, [
		"log",
		"-1",
		COMMIT_SUMMARY_FORMAT,
		ref,
		"--",
	]);
	if (!output?.trim()) return null;
	return parseCommitSummaryLine(output.trim());
}

function getUntrackedDiff(cwd?: string): string {
	const output = runGit(
		cwd,
		["ls-files", "--others", "--exclude-standard", "-z"],
		"Failed to list untracked files.",
	);
	const paths = output.split("\0").filter(Boolean);
	if (paths.length === 0) return "";
	const effectiveCwd = cwd || safeProcessCwd();
	const repoRoot = runGit(
		cwd,
		["rev-parse", "--show-toplevel"],
		"Failed to resolve repository root.",
	).trim();
	return paths
		.map((filePath) => {
			// ls-files returns paths relative to cwd; resolve to repo-relative
			const absPath = path.resolve(effectiveCwd, filePath);
			const repoRelative = path.relative(repoRoot, absPath);
			return buildUntrackedFilePatch(repoRoot, repoRelative);
		})
		.filter((patch): patch is string => patch !== null)
		.join("\n");
}

function buildUntrackedFilePatch(
	repoRoot: string,
	relativePath: string,
): string | null {
	const absolutePath = path.join(repoRoot, relativePath);
	if (!existsSync(absolutePath)) return null;

	let content: string;
	try {
		content = readFileSync(absolutePath, "utf8");
	} catch {
		return null;
	}
	if (content.includes("\0")) return null;

	const lines = splitFileLines(content);
	const lineCount = lines.length;
	const displayPath = relativePath.replace(/\\/g, "/");
	const body = lines.map((line) => `+${line}`).join("\n");
	return [
		`diff --git a/${displayPath} b/${displayPath}`,
		"new file mode 100644",
		"index 0000000..0000000",
		`--- /dev/null`,
		`+++ b/${displayPath}`,
		`@@ -0,0 +1,${lineCount} @@`,
		body,
	]
		.filter((line) => line.length > 0)
		.join("\n");
}

function splitRawDiffIntoFiles(diff: string): string[] {
	if (!diff.trim()) return [];
	return diff
		.split(/(?=^diff --git )/m)
		.filter((chunk) => chunk.trim().startsWith("diff --git "));
}

function splitFileLines(content: string): string[] {
	const normalized = content.replace(/\r\n/g, "\n");
	if (normalized.length === 0) return [];
	const lines = normalized.split("\n");
	if (lines[lines.length - 1] === "") lines.pop();
	return lines;
}

function readWorkingTreeLines(
	repoRoot: string,
	relativePath: string,
): string[] | null {
	const absolutePath = path.join(repoRoot, relativePath);
	if (!existsSync(absolutePath)) return null;
	try {
		return splitFileLines(readFileSync(absolutePath, "utf8"));
	} catch {
		return null;
	}
}

function readGitRevisionLines(
	cwd: string | undefined,
	revision: string,
	relativePath: string,
): string[] | null {
	const output = tryRunGit(cwd, ["show", `${revision}:${relativePath}`]);
	if (output === null) return null;
	return splitFileLines(output);
}

/** Endpoints of a committed diff (commit-vs-parent or branch range). */
export type ReviewRevisions = {
	/** Revision holding the "after" side of the diff. */
	after: string;
	/** Revision holding the "before" side of the diff. */
	before: string;
	/** Stable draft key, e.g. `commit:<sha>` or `branch:<base>:<merge-base>:<head>`. */
	key: string;
};

function loadDisplayLines(options: {
	cwd?: string;
	repoRoot: string;
	file: FileDiffMetadata;
	source: ReviewDiffSource;
	/** Diff endpoints when source is "commit". */
	revisions?: ReviewRevisions;
}): string[] {
	const beforePath = options.file.prevName ?? options.file.name;
	const afterPath = options.file.name;

	switch (options.source) {
		case "working": {
			const afterLines =
				options.file.type === "deleted"
					? null
					: readWorkingTreeLines(options.repoRoot, afterPath);
			const beforeLines =
				options.file.type === "new"
					? null
					: readGitRevisionLines(options.cwd, "HEAD", beforePath);
			return afterLines ?? beforeLines ?? [];
		}
		case "untracked":
			return readWorkingTreeLines(options.repoRoot, afterPath) ?? [];
		case "commit": {
			const revisions = options.revisions;
			if (!revisions) return [];
			// Read the revision snapshots, never the working tree — the
			// filesystem may have moved on since these commits.
			const afterLines =
				options.file.type === "deleted"
					? null
					: readGitRevisionLines(options.cwd, revisions.after, afterPath);
			const beforeLines =
				options.file.type === "new"
					? null
					: readGitRevisionLines(options.cwd, revisions.before, beforePath);
			return afterLines ?? beforeLines ?? [];
		}
	}
}

function buildReviewLinesFromPierreHunk(
	file: FileDiffMetadata,
	hunk: PierreHunk,
): ReviewLine[] {
	const lines: ReviewLine[] = [];
	let nextAdditionLineNumber = hunk.additionStart;
	let nextDeletionLineNumber = hunk.deletionStart;
	for (const block of hunk.hunkContent) {
		if (block.type === "context") {
			for (let index = 0; index < block.lines; index += 1) {
				lines.push({
					kind: "context",
					text: file.additionLines[block.additionLineIndex + index] ?? "",
					additionLineNumber: nextAdditionLineNumber + index,
					deletionLineNumber: nextDeletionLineNumber + index,
				});
			}
			nextAdditionLineNumber += block.lines;
			nextDeletionLineNumber += block.lines;
			continue;
		}
		for (let index = 0; index < block.deletions; index += 1) {
			lines.push({
				kind: "delete",
				text: file.deletionLines[block.deletionLineIndex + index] ?? "",
				deletionLineNumber: nextDeletionLineNumber + index,
			});
		}
		for (let index = 0; index < block.additions; index += 1) {
			lines.push({
				kind: "add",
				text: file.additionLines[block.additionLineIndex + index] ?? "",
				additionLineNumber: nextAdditionLineNumber + index,
			});
		}
		nextDeletionLineNumber += block.deletions;
		nextAdditionLineNumber += block.additions;
	}
	return lines;
}

function hunkToReviewHunk(
	file: FileDiffMetadata,
	hunk: PierreHunk,
	fileNoteKey: string,
	index: number,
	rawPatch: string,
): ReviewHunk {
	let cachedLines: ReviewLine[] | null = null;
	const noteKey = `${fileNoteKey}:${hunk.hunkSpecs ?? `hunk-${index + 1}`}:${index}`;
	const header = hunk.hunkSpecs ?? `Hunk ${index + 1}`;
	return {
		id: `${file.name}:${hunk.hunkSpecs ?? index}:${index}`,
		noteKey,
		header,
		context: hunk.hunkContext ?? "",
		get lines() {
			cachedLines ??= buildReviewLinesFromPierreHunk(file, hunk);
			return cachedLines;
		},
		changeCount: hunk.additionLines + hunk.deletionLines,
		rawPatch,
		patchStartLine: hunk.unifiedLineStart,
		patchLineCount: hunk.unifiedLineCount,
		additionStart: hunk.additionStart,
		additionCount: hunk.additionCount,
		deletionStart: hunk.deletionStart,
		deletionCount: hunk.deletionCount,
		collapsedBefore: hunk.collapsedBefore,
	};
}

function splitRawPatchIntoHunks(rawPatch: string): string[] {
	const lines = rawPatch.replace(/\r\n/g, "\n").split("\n");
	const firstHunkIndex = lines.findIndex((line) => line.startsWith("@@ "));
	if (firstHunkIndex < 0) return [];
	const headerLines = lines.slice(0, firstHunkIndex);
	const hunks: string[] = [];
	let current: string[] = [];
	for (const line of lines.slice(firstHunkIndex)) {
		if (line.startsWith("@@ ") && current.length > 0) {
			hunks.push([...headerLines, ...current].join("\n"));
			current = [line];
			continue;
		}
		current.push(line);
	}
	if (current.length > 0) {
		hunks.push([...headerLines, ...current].join("\n"));
	}
	return hunks;
}

function extractRawPatchHeader(rawPatch: string): string[] {
	const lines = rawPatch.replace(/\r\n/g, "\n").split("\n");
	const firstHunkIndex = lines.findIndex((line) => line.startsWith("@@ "));
	if (firstHunkIndex < 0) return lines;
	return lines.slice(0, firstHunkIndex);
}

function formatHunkSpan(start: number, count: number): string {
	return count === 1 ? `${start}` : `${start},${count}`;
}

function buildSkippedSectionPatch(
	headerLines: string[],
	lines: string[],
	additionStart: number,
	deletionStart: number,
): string {
	const count = lines.length;
	return [
		...headerLines,
		`@@ -${formatHunkSpan(deletionStart, count)} +${formatHunkSpan(additionStart, count)} @@`,
		...lines.map((line) => ` ${line}`),
	].join("\n");
}

export function buildSkippedSectionsForFile(
	fileId: string,
	rawPatch: string,
	hunks: ReviewHunk[],
	displayLines: string[],
): ReviewSkippedSection[] {
	if (hunks.length === 0 || displayLines.length === 0) return [];
	const headerLines = extractRawPatchHeader(rawPatch);
	const sections: ReviewSkippedSection[] = [];

	for (const [index, hunk] of hunks.entries()) {
		if (hunk.collapsedBefore <= 0) continue;
		const additionStart = hunk.additionStart - hunk.collapsedBefore;
		const deletionStart = hunk.deletionStart - hunk.collapsedBefore;
		const displayStart = additionStart > 0 ? additionStart : deletionStart;
		const gapLines = displayLines.slice(
			Math.max(0, displayStart - 1),
			Math.max(0, displayStart - 1) + hunk.collapsedBefore,
		);
		if (gapLines.length === 0) continue;
		sections.push({
			id: `${fileId}:gap:${index}`,
			beforeHunkIndex: index,
			rawPatch: buildSkippedSectionPatch(
				headerLines,
				gapLines,
				additionStart,
				deletionStart,
			),
			lineCount: gapLines.length,
			additionStart,
			deletionStart,
		});
	}

	const lastHunk = hunks[hunks.length - 1];
	const trailingAdditionStart = lastHunk.additionStart + lastHunk.additionCount;
	const trailingDeletionStart = lastHunk.deletionStart + lastHunk.deletionCount;
	const trailingDisplayStart =
		trailingAdditionStart > 0 ? trailingAdditionStart : trailingDeletionStart;
	const trailingLines = displayLines.slice(
		Math.max(0, trailingDisplayStart - 1),
	);
	if (trailingLines.length > 0) {
		sections.push({
			id: `${fileId}:gap:${hunks.length}`,
			beforeHunkIndex: hunks.length,
			rawPatch: buildSkippedSectionPatch(
				headerLines,
				trailingLines,
				trailingAdditionStart,
				trailingDeletionStart,
			),
			lineCount: trailingLines.length,
			additionStart: trailingAdditionStart,
			deletionStart: trailingDeletionStart,
		});
	}

	return sections;
}

const EAGER_SKIPPED_SECTIONS_FILE_LIMIT = 50;

function fileToReviewFile(
	file: FileDiffMetadata,
	rawPatch: string,
	index: number,
	options: {
		cwd?: string;
		repoRoot: string;
		source: ReviewDiffSource;
		includeSkippedSections: boolean;
		/** Diff endpoints when source is "commit". */
		revisions?: ReviewRevisions;
	},
): ReviewFile {
	// Committed-diff notes are scoped by their revision key so drafts on
	// different targets never collide
	// (see docs/features/code-review-commit-targets.md).
	const sourceKey =
		options.source === "commit" && options.revisions
			? options.revisions.key
			: options.source;
	const noteKey = `${sourceKey}:${file.prevName ?? ""}->${file.name}`;
	const rawHunks = splitRawPatchIntoHunks(rawPatch);
	const hunks = file.hunks.map((hunk, hunkIndex) =>
		hunkToReviewHunk(
			file,
			hunk,
			noteKey,
			hunkIndex,
			rawHunks[hunkIndex] ?? rawPatch,
		),
	);
	const changeCount = hunks.reduce((sum, hunk) => sum + hunk.changeCount, 0);
	const id = `${noteKey}:${index}`;
	const skippedSections = options.includeSkippedSections
		? buildSkippedSectionsForFile(
				id,
				rawPatch,
				hunks,
				loadDisplayLines({
					cwd: options.cwd,
					repoRoot: options.repoRoot,
					file,
					source: options.source,
					revisions: options.revisions,
				}),
			)
		: [];
	return {
		id,
		noteKey,
		path: file.name,
		prevPath: file.prevName,
		status: file.type,
		source: options.source,
		filetype: inferFiletype(file.name),
		rawPatch,
		hunks,
		skippedSections,
		changeCount,
		unifiedLineCount: file.unifiedLineCount,
		splitLineCount: file.splitLineCount,
	};
}

type ReviewPatchSet = {
	source: ReviewDiffSource;
	files: FileDiffMetadata[];
	rawFiles: string[];
};

function parseReviewPatchSet(
	diff: string,
	source: ReviewDiffSource,
): ReviewPatchSet | null {
	if (!diff.trim()) return null;
	const parsed = parsePatchFiles(diff, "review", true);
	return {
		source,
		files: parsed.flatMap((patch) => patch.files),
		rawFiles: splitRawDiffIntoFiles(diff),
	};
}

function yieldToRenderer(): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, 0));
}

function reviewFilesFromPatchSets(options: {
	cwd?: string;
	repoRoot: string;
	revisions?: ReviewRevisions;
	patchSets: ReviewPatchSet[];
}): ReviewFile[] {
	const totalFileCount = options.patchSets.reduce(
		(count, patchSet) => count + patchSet.files.length,
		0,
	);
	const includeSkippedSections =
		totalFileCount <= EAGER_SKIPPED_SECTIONS_FILE_LIMIT;
	const reviewFiles: ReviewFile[] = [];
	for (const patchSet of options.patchSets) {
		for (const [index, file] of patchSet.files.entries()) {
			reviewFiles.push(
				fileToReviewFile(
					file,
					patchSet.rawFiles[index] ?? "",
					reviewFiles.length,
					{
						cwd: options.cwd,
						repoRoot: options.repoRoot,
						source: patchSet.source,
						includeSkippedSections,
						revisions: options.revisions,
					},
				),
			);
		}
	}
	return reviewFiles;
}

export async function loadReviewFiles(
	cwd?: string,
	target: ReviewTarget = { kind: "working" },
): Promise<ReviewFile[]> {
	await yieldToRenderer();
	const repoRoot = getGitRepoRoot(cwd);
	if (!repoRoot) return [];
	const revisions = revisionsForTarget(cwd, target);
	const patchSets = (
		revisions
			? [
					parseReviewPatchSet(
						getRevisionDiff(cwd, revisions.before, revisions.after),
						"commit",
					),
				]
			: [
					parseReviewPatchSet(getWorkingTreeDiff(cwd), "working"),
					parseReviewPatchSet(getUntrackedDiff(cwd), "untracked"),
				]
	).filter((value): value is ReviewPatchSet => value !== null);
	if (patchSets.length === 0) return [];
	return reviewFilesFromPatchSets({
		cwd,
		repoRoot,
		revisions: revisions ?? undefined,
		patchSets,
	});
}

/** Load an explicit immutable revision range, including branch merge-base diffs. */
export async function loadReviewFilesForRevisions(
	cwd: string | undefined,
	before: string,
	after: string,
): Promise<ReviewFile[]> {
	await yieldToRenderer();
	const repoRoot = getGitRepoRoot(cwd);
	if (!repoRoot) return [];
	const revisions: ReviewRevisions = {
		before,
		after,
		key: `range:${before}:${after}`,
	};
	const patchSet = parseReviewPatchSet(
		getRevisionDiff(cwd, before, after),
		"commit",
	);
	if (!patchSet) return [];
	return reviewFilesFromPatchSets({
		cwd,
		repoRoot,
		revisions,
		patchSets: [patchSet],
	});
}

/** Resolve the project root for the given working directory. */
export function getRepoRoot(cwd?: string): string {
	return getGitRepoRoot(cwd) ?? (cwd || safeProcessCwd());
}

const FILE_LIST_IGNORED_DIRS = new Set([
	".git",
	"node_modules",
	".next",
	"dist",
	"build",
	"out",
]);

function listDirectoryFiles(root: string): string[] {
	const files: string[] = [];
	function visit(dir: string) {
		let entries: Dirent[];
		try {
			entries = readdirSync(dir, { withFileTypes: true });
		} catch {
			return;
		}
		for (const entry of entries) {
			const absolutePath = path.join(dir, entry.name);
			const relativePath = path
				.relative(root, absolutePath)
				.replace(/\\/g, "/");
			if (entry.isDirectory()) {
				if (!FILE_LIST_IGNORED_DIRS.has(entry.name)) visit(absolutePath);
				continue;
			}
			if (entry.isFile()) files.push(relativePath);
		}
	}
	visit(root);
	return files;
}

/** List project files, using git when available and the filesystem otherwise. */
export function listRepoFiles(cwd?: string): string[] {
	const effectiveCwd = cwd || safeProcessCwd();
	if (!getGitRepoRoot(cwd)) return listDirectoryFiles(effectiveCwd);
	const result = spawnSync("git", ["ls-files", "--full-name"], {
		cwd: effectiveCwd,
		encoding: "utf8",
		maxBuffer: 10 * 1024 * 1024,
	});
	if (result.status !== 0) return [];
	return result.stdout.trim().split("\n").filter(Boolean);
}
