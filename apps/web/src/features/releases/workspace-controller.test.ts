import { describe, expect, test } from "bun:test";
import { createReleasesWorkspaceController } from "./workspace-controller";

const UPDATE = {
	version: "0.26.0",
	tag: "v0.26.0",
	url: "https://github.com/akonwi/kit/releases/tag/v0.26.0",
	notes: "new notes",
	publishedAt: "2026-03-01T00:00:00Z",
};

describe("releases workspace controller", () => {
	test("checks once and exposes the newer release notes", async () => {
		let checks = 0;
		const controller = createReleasesWorkspaceController({
			currentVersion: "0.25.0",
			currentNotes: "installed notes",
			checkLatest: async () => {
				checks += 1;
				return UPDATE;
			},
		});
		const states: string[] = [];
		controller.subscribe((state) => {
			states.push(
				state.checking ? "checking" : (state.latest?.version ?? "idle"),
			);
		});

		await Promise.all([
			controller.checkForUpdate(),
			controller.checkForUpdate(),
		]);
		await controller.checkForUpdate();

		expect(checks).toBe(1);
		expect(states).toEqual(["checking", "0.26.0"]);
		expect(controller.getState().releases[0]?.notes).toBe("new notes");
		expect(controller.getState().releases[1]?.notes).toBe("installed notes");
	});

	test("loads release history lazily and preserves bundled installed notes", async () => {
		let loads = 0;
		const controller = createReleasesWorkspaceController({
			currentVersion: "0.25.0",
			currentNotes: "bundled notes",
			fetchHistoryPage: async () => {
				loads += 1;
				return {
					releases: [
						UPDATE,
						{
							version: "0.25.0",
							tag: "v0.25.0",
							url: "release-url",
							notes: "remote notes",
							publishedAt: "2026-02-01T00:00:00Z",
						},
					],
					hasMore: false,
				};
			},
		});

		expect(controller.getState().historyStatus).toBe("idle");
		await Promise.all([controller.loadReleases(), controller.loadReleases()]);
		await controller.loadReleases();

		expect(loads).toBe(1);
		expect(controller.getState().historyStatus).toBe("loaded");
		expect(controller.getState().latest?.version).toBe("0.26.0");
		expect(controller.getState().releases).toHaveLength(2);
		expect(controller.getState().releases[1]).toMatchObject({
			url: "release-url",
			notes: "bundled notes",
			publishedAt: "2026-02-01T00:00:00Z",
		});
	});

	test("loads three releases at a time and preserves earlier pages", async () => {
		const pages: number[] = [];
		const controller = createReleasesWorkspaceController({
			currentVersion: "0.28.0",
			fetchHistoryPage: async (_version, page) => {
				pages.push(page);
				return page === 1
					? {
							releases: [
								{
									version: "0.28.0",
									tag: "v0.28.0",
									url: "28-url",
									notes: "28 notes",
									publishedAt: "2026-03-03T00:00:00Z",
								},
								{
									version: "0.27.1",
									tag: "v0.27.1",
									url: "271-url",
									notes: "271 notes",
									publishedAt: "2026-03-02T00:00:00Z",
								},
							],
							hasMore: true,
						}
					: {
							releases: [
								{
									version: "0.27.0",
									tag: "v0.27.0",
									url: "27-url",
									notes: "27 notes",
									publishedAt: "2026-03-01T00:00:00Z",
								},
							],
							hasMore: false,
						};
			},
		});

		await controller.loadReleases();
		expect(controller.getState().hasMore).toBe(true);
		await controller.loadMoreReleases();

		expect(pages).toEqual([1, 2]);
		expect(
			controller.getState().releases.map((release) => release.version),
		).toEqual(["0.28.0", "0.27.1", "0.27.0"]);
		expect(controller.getState().hasMore).toBe(false);
	});

	test("sorts releases by publication date rather than semantic version", async () => {
		const controller = createReleasesWorkspaceController({
			currentVersion: "0.28.0",
			fetchHistoryPage: async () => ({
				releases: [
					{
						version: "0.30.0",
						tag: "v0.30.0",
						url: "30-url",
						notes: "30 notes",
						publishedAt: "2026-03-01T00:00:00Z",
					},
					{
						version: "0.29.0",
						tag: "v0.29.0",
						url: "29-url",
						notes: "29 notes",
						publishedAt: "2026-03-02T00:00:00Z",
					},
				],
				hasMore: false,
			}),
		});

		await controller.loadReleases();

		expect(
			controller.getState().releases.map((release) => release.version),
		).toEqual(["0.29.0", "0.30.0", "0.28.0"]);
	});

	test("keeps the newest known update regardless of request completion order", async () => {
		const controller = createReleasesWorkspaceController({
			currentVersion: "0.25.0",
			checkLatest: async () => null,
			fetchHistoryPage: async () => ({
				releases: [
					{
						version: "0.27.0",
						tag: "v0.27.0",
						url: "newest-url",
						notes: "newest notes",
						publishedAt: "2026-03-02T00:00:00Z",
					},
				],
				hasMore: false,
			}),
		});

		await controller.loadReleases();
		await controller.checkForUpdate();

		expect(controller.getState().latest?.version).toBe("0.27.0");
	});

	test("times out stalled requests and aborts requests on disposal", async () => {
		let aborts = 0;
		const createController = () =>
			createReleasesWorkspaceController({
				checkTimeoutMs: 1,
				checkLatest: (_version, signal) =>
					new Promise((_resolve, reject) => {
						signal.addEventListener("abort", () => {
							aborts += 1;
							reject(new Error("aborted"));
						});
					}),
			});

		const timedOut = createController();
		await timedOut.checkForUpdate();
		expect(timedOut.getState().checking).toBe(false);

		const disposed = createController();
		const pending = disposed.checkForUpdate();
		disposed.dispose();
		await pending;
		expect(aborts).toBe(2);
	});

	test("fails silently and opens the panel while requesting history", async () => {
		const controller = createReleasesWorkspaceController({
			checkLatest: async () => {
				throw new Error("offline");
			},
			fetchHistoryPage: async () => {
				throw new Error("offline");
			},
		});
		let opens = 0;
		const unsubscribe = controller.onOpenRequest(() => {
			opens += 1;
		});

		await controller.checkForUpdate();
		controller.open();
		await controller.loadReleases();
		unsubscribe();
		controller.open();
		await controller.loadReleases();

		expect(controller.getState().latest).toBeNull();
		expect(controller.getState().checking).toBe(false);
		expect(controller.getState().historyStatus).toBe("unavailable");
		expect(opens).toBe(1);
	});
});
