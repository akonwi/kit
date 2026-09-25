import { describe, expect, test } from "bun:test";
import {
	checkLatestRelease,
	fetchReleasePage,
	isNewerVersion,
} from "./release-check";

describe("release update checks", () => {
	test("compares stable semantic versions", () => {
		expect(isNewerVersion("v0.26.0", "0.25.0")).toBe(true);
		expect(isNewerVersion("0.25.1", "v0.25.0")).toBe(true);
		expect(isNewerVersion("v0.25.0", "0.25.0")).toBe(false);
		expect(isNewerVersion("v0.24.9", "0.25.0")).toBe(false);
		expect(isNewerVersion("v1.0.0", "1.0.0-beta.1")).toBe(true);
		expect(isNewerVersion("v1.1.0-beta.1", "1.0.0")).toBe(false);
		expect(isNewerVersion("v01.0.0", "1.0.0")).toBe(false);
		expect(isNewerVersion("latest", "0.25.0")).toBe(false);
	});

	test("returns the newer release with its published notes", async () => {
		const fetchImpl = async () =>
			new Response(
				JSON.stringify({
					tag_name: "v0.26.0",
					body: "## New release",
					html_url: "https://github.com/akonwi/kit/releases/tag/v0.26.0",
					published_at: "2026-03-01T12:00:00Z",
				}),
				{ status: 200 },
			);

		await expect(checkLatestRelease("0.25.0", fetchImpl)).resolves.toEqual({
			version: "0.26.0",
			tag: "v0.26.0",
			url: "https://github.com/akonwi/kit/releases/tag/v0.26.0",
			notes: "## New release",
			publishedAt: "2026-03-01T12:00:00Z",
		});
	});

	test("returns null for the installed release", async () => {
		const fetchImpl = async () =>
			new Response(JSON.stringify({ tag_name: "v0.25.0" }), {
				status: 200,
			});
		expect(await checkLatestRelease("0.25.0", fetchImpl)).toBeNull();
	});

	test("lists stable published releases with their notes", async () => {
		const fetchImpl = async () =>
			new Response(
				JSON.stringify([
					{ tag_name: "0.26.0", body: "new notes" },
					{ tag_name: "v0.27.0-beta.1", body: "preview" },
					{ tag_name: "v0.25.0", body: "old notes", draft: true },
				]),
				{
					status: 200,
					headers: {
						Link: '<https://api.github.com/releases?page=2>; rel="next"',
					},
				},
			);

		expect(await fetchReleasePage("0.25.0", 1, fetchImpl)).toEqual({
			releases: [
				{
					version: "0.26.0",
					tag: "v0.26.0",
					url: "https://github.com/akonwi/kit/releases/tag/0.26.0",
					notes: "new notes",
				},
			],
			hasMore: true,
		});
	});

	test("does not advertise another page without a GitHub next link", async () => {
		const fetchImpl = async () =>
			new Response(
				JSON.stringify([
					{ tag_name: "v0.25.0" },
					{ tag_name: "v0.24.0" },
					{ tag_name: "v0.23.0" },
				]),
				{ status: 200 },
			);

		expect((await fetchReleasePage("0.25.0", 2, fetchImpl)).hasMore).toBe(
			false,
		);
	});

	test("filters malformed entries instead of failing the page", async () => {
		const fetchImpl = async () =>
			new Response(JSON.stringify([null, "invalid", { tag_name: "v0.25.0" }]), {
				status: 200,
				headers: {
					Link: '<https://api.github.com/releases?page=3>; rel="next"',
				},
			});

		const page = await fetchReleasePage("0.25.0", 2, fetchImpl);
		expect(page.releases.map((release) => release.tag)).toEqual(["v0.25.0"]);
		expect(page.hasMore).toBe(true);
	});
});
