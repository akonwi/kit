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

		expect(await controller.activateContribution("plugin:ci")).toBe(true);
		expect(clicked).toBe(true);
		expect(await controller.activateContribution("missing")).toBe(false);
	});

	test("normalizes declarative URL actions and rejects ambiguous actions", () => {
		const controller = createChromeContributionsController();
		controller.setContribution({
			id: "git.pr",
			content: "PR #25",
			action: {
				type: "open-url",
				url: "https://github.com/owner/repo/pull/25",
			},
		});
		expect(controller.getContributions()[0]?.action).toEqual({
			type: "open-url",
			url: "https://github.com/owner/repo/pull/25",
		});
		expect(() =>
			controller.setContribution({
				id: "bad.protocol",
				content: "bad",
				action: { type: "open-url", url: "javascript:alert(1)" },
			}),
		).toThrow("must use HTTP or HTTPS");
		expect(() =>
			controller.setContribution({
				id: "bad.ambiguous",
				content: "bad",
				action: { type: "open-url", url: "https://example.com" },
				onClick: () => {},
			}),
		).toThrow("cannot define both");
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
