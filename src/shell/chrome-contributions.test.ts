import { describe, expect, test } from "bun:test";
import { createChromeContributionsController } from "./chrome-contributions";

describe("createChromeContributionsController", () => {
	test("tracks hidden contribution ids", () => {
		const controller = createChromeContributionsController();

		expect(controller.isHidden("HeaderBar:model")).toBe(false);
		const restore = controller.hideContribution("HeaderBar:model");
		expect(controller.isHidden("HeaderBar:model")).toBe(true);

		restore();
		expect(controller.isHidden("HeaderBar:model")).toBe(false);
	});
});
