import { describe, expect, test } from "bun:test";
import { checkLatestRelease, isNewerVersion } from "./release-check";

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

	test("returns trusted metadata only when GitHub reports a newer release", async () => {
		const fetchImpl = async () =>
			new Response(JSON.stringify({ tag_name: "v0.26.0" }), {
				status: 200,
			});

		await expect(checkLatestRelease("0.25.0", fetchImpl)).resolves.toEqual({
			version: "0.26.0",
			tag: "v0.26.0",
			url: "https://github.com/akonwi/kit/releases/tag/v0.26.0",
		});
	});

	test("returns null for the installed release", async () => {
		const fetchImpl = async () =>
			new Response(JSON.stringify({ tag_name: "v0.25.0" }), {
				status: 200,
			});
		expect(await checkLatestRelease("0.25.0", fetchImpl)).toBeNull();
	});
});
