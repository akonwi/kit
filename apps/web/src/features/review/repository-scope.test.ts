import { describe, expect, test } from "bun:test";
import {
	createReviewRepositoryScope,
	type ReviewRepositoryChange,
} from "./repository-scope";

describe("review repository scope", () => {
	test("tracks cwd changes and active session changes", () => {
		let listener: ((event: ReviewRepositoryChange) => void) | undefined;
		let disposed = false;
		const scope = createReviewRepositoryScope({
			initialCwd: "/repo-a/packages/app",
			subscribe: (next) => {
				listener = next;
				return () => {
					disposed = true;
				};
			},
			resolveRepoRoot: (cwd) => cwd.split("/packages/")[0] ?? cwd,
		});

		expect(scope.repoRoot()).toBe("/repo-a");
		listener?.({
			type: "session.active.changed.cwd",
			session: { cwd: "/repo-b" },
			cwd: "/repo-b",
		});
		expect(scope.repoRoot()).toBe("/repo-b");

		listener?.({
			type: "session.active.changed",
			session: { cwd: "/repo-c/packages/api" },
		});
		expect(scope.repoRoot()).toBe("/repo-c");

		scope.dispose();
		expect(disposed).toBe(true);
	});
});
