import { describe, expect, test } from "bun:test";
import { parsePullRequest } from "./pr";

describe("parsePullRequest", () => {
	test("parses gh pr view JSON", () => {
		expect(
			parsePullRequest(
				JSON.stringify({
					number: 123,
					title: "Add feature",
					url: "https://github.com/owner/repo/pull/123",
					headRefName: "feature",
					baseRefName: "main",
				}),
			),
		).toEqual({
			number: 123,
			title: "Add feature",
			url: "https://github.com/owner/repo/pull/123",
			headRefName: "feature",
			baseRefName: "main",
		});
	});

	test("returns null for invalid JSON", () => {
		expect(parsePullRequest("not json")).toBeNull();
	});

	test("returns null when the PR number is missing", () => {
		expect(
			parsePullRequest(JSON.stringify({ title: "missing number" })),
		).toBeNull();
	});
});
