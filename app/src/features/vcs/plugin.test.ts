import { describe, expect, test } from "bun:test";
import { isPullRequestCacheFresh } from "./plugin";

describe("VCS pull request cache", () => {
	test("scopes cached PR links to both repository cwd and branch", () => {
		const cache = {
			cwd: "/workspace/one",
			branch: "main",
			pullRequest: null,
			updatedAt: 1_000,
		};

		expect(
			isPullRequestCacheFresh(cache, "/workspace/one", "main", 2_000),
		).toBe(true);
		expect(
			isPullRequestCacheFresh(cache, "/workspace/two", "main", 2_000),
		).toBe(false);
		expect(
			isPullRequestCacheFresh(cache, "/workspace/one", "feature", 2_000),
		).toBe(false);
		expect(
			isPullRequestCacheFresh(cache, "/workspace/one", "main", 62_000),
		).toBe(false);
	});
});
