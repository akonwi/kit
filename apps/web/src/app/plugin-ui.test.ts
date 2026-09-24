import { describe, expect, test } from "bun:test";
import { measurePluginConfirmDialogWidth } from "./plugin-ui";

describe("plugin confirmation dialog sizing", () => {
	test("grows with confirmation content up to its width limit", () => {
		expect(measurePluginConfirmDialogWidth({ title: "Confirm?" })).toBe(44);

		const medium = measurePluginConfirmDialogWidth({
			title: "Plugin UI demo: confirm",
			message: "Show an info toast for the whole project?",
			confirmLabel: "Show toast",
			cancelLabel: "Cancel",
		});
		expect(medium).toBeGreaterThan(44);
		expect(medium).toBeLessThan(96);

		expect(
			measurePluginConfirmDialogWidth({
				title: "Confirm?",
				message: "x".repeat(200),
			}),
		).toBe(96);
	});

	test("uses the longest message line rather than total message length", () => {
		expect(
			measurePluginConfirmDialogWidth({
				title: "Confirm?",
				message: ["short", "also short", "short again"].join("\n"),
			}),
		).toBe(44);
	});
});
