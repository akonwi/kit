import { describe, expect, test } from "bun:test";
import { createReleasesWorkspaceController } from "./workspace-controller";

describe("releases workspace controller", () => {
	test("checks once per app controller and publishes a newer release", async () => {
		let checks = 0;
		const controller = createReleasesWorkspaceController({
			currentVersion: "0.25.0",
			currentNotes: "notes",
			checkLatest: async () => {
				checks += 1;
				return {
					version: "0.26.0",
					tag: "v0.26.0",
					url: "https://github.com/akonwi/kit/releases/tag/v0.26.0",
				};
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
		expect(controller.getState().currentNotes).toBe("notes");
	});

	test("times out stalled checks and aborts checks on disposal", async () => {
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

	test("fails silently and notifies open requests", async () => {
		const controller = createReleasesWorkspaceController({
			checkLatest: async () => {
				throw new Error("offline");
			},
		});
		let opens = 0;
		const unsubscribe = controller.onOpenRequest(() => {
			opens += 1;
		});

		await controller.checkForUpdate();
		controller.open();
		unsubscribe();
		controller.open();

		expect(controller.getState().latest).toBeNull();
		expect(controller.getState().checking).toBe(false);
		expect(opens).toBe(1);
	});
});
