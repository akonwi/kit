import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { messagePartToPromptText } from "../../messages/parts";
import {
	getCurrentBranch,
	getMergeBase,
	isAncestorOfHead,
	listLocalBranches,
	listRecentCommits,
	loadReviewFiles,
	resolveCommit,
	resolveCommitParent,
	resolveDefaultBranchBase,
} from "./model";

let repo: string;
let firstSha: string;
let secondSha: string;

function git(args: string[]): string {
	return execFileSync("git", args, {
		cwd: repo,
		encoding: "utf8",
		// Isolate from the developer's global/system git config
		// (init.defaultBranch, commit.gpgsign, hooks, …).
		env: {
			...process.env,
			GIT_CONFIG_GLOBAL: "/dev/null",
			GIT_CONFIG_SYSTEM: "/dev/null",
		},
	}).trim();
}

beforeAll(() => {
	repo = mkdtempSync(path.join(tmpdir(), "kit-review-commit-"));
	git(["init", "-q", "-b", "main"]);
	git(["config", "user.email", "test@example.com"]);
	git(["config", "user.name", "Test"]);
	writeFileSync(path.join(repo, "alpha.txt"), "one\ntwo\nthree\n");
	git(["add", "."]);
	git(["commit", "-q", "-m", "feat: add alpha"]);
	firstSha = git(["rev-parse", "HEAD"]);
	writeFileSync(path.join(repo, "alpha.txt"), "one\nTWO\nthree\nfour\n");
	git(["add", "."]);
	git(["commit", "-q", "-m", "fix: revise alpha"]);
	secondSha = git(["rev-parse", "HEAD"]);
});

afterAll(() => {
	rmSync(repo, { recursive: true, force: true });
});

describe("commit review targets", () => {
	test("listRecentCommits returns newest first with subjects", () => {
		const commits = listRecentCommits(repo);
		expect(commits.length).toBe(2);
		expect(commits[0].sha).toBe(secondSha);
		expect(commits[0].shortSha.length).toBeLessThan(secondSha.length);
		expect(secondSha.startsWith(commits[0].shortSha)).toBe(true);
		expect(commits[0].subject).toBe("fix: revise alpha");
		expect(commits[1].subject).toBe("feat: add alpha");
	});

	test("resolveCommit resolves HEAD and rejects unknown refs", () => {
		const head = resolveCommit(repo, "HEAD");
		expect(head?.sha).toBe(secondSha);
		expect(resolveCommit(repo, "not-a-ref")).toBeNull();
	});

	test("resolveCommitParent falls back to the empty tree for root commits", () => {
		expect(resolveCommitParent(repo, secondSha)).toBe(firstSha);
		expect(resolveCommitParent(repo, firstSha)).toBe(
			"4b825dc642cb6eb9a060e54bf8d69288fbee4904",
		);
	});

	test("isAncestorOfHead detects amended-away commits", () => {
		expect(isAncestorOfHead(repo, firstSha)).toBe(true);
		expect(isAncestorOfHead(repo, secondSha)).toBe(true);

		git(["checkout", "-q", "-b", "amend-me"]);
		try {
			writeFileSync(path.join(repo, "gamma.txt"), "draft\n");
			git(["add", "."]);
			git(["commit", "-q", "-m", "feat: gamma"]);
			const originalSha = git(["rev-parse", "HEAD"]);
			expect(isAncestorOfHead(repo, originalSha)).toBe(true);

			writeFileSync(path.join(repo, "gamma.txt"), "amended\n");
			git(["add", "."]);
			git(["commit", "-q", "--amend", "-m", "feat: gamma (amended)"]);
			// The original commit still exists in the reflog but is no
			// longer part of HEAD's history — the staleness signal.
			expect(isAncestorOfHead(repo, originalSha)).toBe(false);
		} finally {
			git(["checkout", "-q", "main"]);
			git(["branch", "-q", "-D", "amend-me"]);
		}
	});

	test("loadReviewFiles with a commit target diffs commit vs parent", async () => {
		const files = await loadReviewFiles(repo, {
			kind: "commit",
			sha: secondSha,
		});
		expect(files.length).toBe(1);
		const file = files[0];
		expect(file.path).toBe("alpha.txt");
		expect(file.source).toBe("commit");
		// Note keys are sha-scoped so drafts never collide across targets.
		expect(file.noteKey.startsWith(`commit:${secondSha}:`)).toBe(true);
		const patch = file.rawPatch;
		expect(patch).toContain("+TWO");
		expect(patch).toContain("-two");
		expect(patch).toContain("+four");
	});

	test("root commit target diffs against the empty tree", async () => {
		const files = await loadReviewFiles(repo, {
			kind: "commit",
			sha: firstSha,
		});
		expect(files.length).toBe(1);
		expect(files[0].status).toBe("new");
		expect(files[0].rawPatch).toContain("+one");
	});

	test("commit target ignores working tree changes", async () => {
		writeFileSync(path.join(repo, "beta.txt"), "uncommitted\n");
		try {
			const files = await loadReviewFiles(repo, {
				kind: "commit",
				sha: secondSha,
			});
			expect(files.map((f) => f.path)).toEqual(["alpha.txt"]);
		} finally {
			rmSync(path.join(repo, "beta.txt"), { force: true });
		}
	});

	test("branch target diffs the whole branch against the merge base", async () => {
		git(["checkout", "-q", "-b", "feature"]);
		try {
			writeFileSync(path.join(repo, "beta.txt"), "branch work\n");
			git(["add", "."]);
			git(["commit", "-q", "-m", "feat: add beta"]);
			writeFileSync(path.join(repo, "beta.txt"), "branch work\nmore\n");
			git(["add", "."]);
			git(["commit", "-q", "-m", "feat: extend beta"]);
			const head = git(["rev-parse", "HEAD"]);

			expect(getCurrentBranch(repo)).toBe("feature");
			expect(listLocalBranches(repo).map((b) => b.name)).toEqual(["main"]);
			// Repo has no origin; falls back to the local main branch.
			const base = resolveDefaultBranchBase(repo);
			expect(base).toBe("main");
			const baseRef = base as string;
			const mergeBase = getMergeBase(repo, baseRef, head);
			expect(mergeBase).toBe(secondSha);

			const files = await loadReviewFiles(repo, {
				kind: "branch",
				base: baseRef,
				head,
				mergeBase: mergeBase as string,
			});
			// Total branch diff: both commits folded into one file diff.
			expect(files.length).toBe(1);
			expect(files[0].path).toBe("beta.txt");
			expect(files[0].rawPatch).toContain("+branch work");
			expect(files[0].rawPatch).toContain("+more");
			expect(
				files[0].noteKey.startsWith(`branch:${baseRef}:${mergeBase}:${head}:`),
			).toBe(true);
		} finally {
			git(["checkout", "-q", "-"]);
			git(["branch", "-q", "-D", "feature"]);
		}
	});

	test("resolveDefaultBranchBase returns null when base is the current commit", () => {
		// On the default branch itself with no origin, main === HEAD.
		expect(resolveDefaultBranchBase(repo)).toBeNull();
	});

	test("prompt text states the commit scope", () => {
		const text = messagePartToPromptText({
			type: "code-review",
			review: {
				submittedAt: new Date().toISOString(),
				files: [
					{
						path: "alpha.txt",
						fileComment: "",
						ranges: [
							{
								side: "additions",
								startLine: 2,
								endLine: 2,
								comment: "why uppercase?",
							},
						],
					},
				],
				commit: {
					sha: secondSha,
					parentSha: firstSha,
					subject: "fix: revise alpha",
				},
			},
		});
		expect(text).toContain(`${firstSha}..${secondSha}`);
		expect(text).toContain(`git diff ${firstSha} ${secondSha}`);
		expect(text).toContain("why uppercase?");
	});
});
