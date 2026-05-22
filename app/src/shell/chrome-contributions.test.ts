import { describe, expect, test } from "bun:test";
import { createChromeContributionsController } from "./chrome-contributions";

describe("createChromeContributionsController", () => {
	test("stores styled content and click handlers", async () => {
		const controller = createChromeContributionsController();
		let clicked = false;

		controller.setContribution({
			id: "plugin:ci",
			content: [
				{ text: " tests ", style: { fg: "green", bold: true } },
				" passing ",
			],
			onClick: () => {
				clicked = true;
			},
		});

		const [contribution] = controller.getContributions();
		expect(contribution.plainText).toBe("tests  passing");
		expect(contribution.content).toEqual([
			{ text: "tests ", style: { fg: "green", bold: true } },
			{ text: " passing" },
		]);

		await contribution.onClick?.();
		expect(clicked).toBe(true);
	});

	test("tracks hidden contribution ids", () => {
		const controller = createChromeContributionsController();

		expect(controller.isHidden("HeaderBar:model")).toBe(false);
		const restore = controller.hideContribution("HeaderBar:model");
		expect(controller.isHidden("HeaderBar:model")).toBe(true);

		restore();
		expect(controller.isHidden("HeaderBar:model")).toBe(false);
	});
});
