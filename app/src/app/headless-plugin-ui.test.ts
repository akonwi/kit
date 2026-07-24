import { describe, expect, test } from "bun:test";
import { createHeadlessPluginUI } from "./headless-plugin-ui";

describe("createHeadlessPluginUI", () => {
	test("returns inert values for UI interactions", async () => {
		const ui = createHeadlessPluginUI();
		expect(
			await ui.select({ title: "Choose", options: ["one", "two"] }),
		).toBeUndefined();
		expect(await ui.input({ title: "Enter" })).toBeUndefined();
		await expect(ui.confirm({ title: "Confirm" })).rejects.toThrow(
			"interactivity is unavailable",
		);
		await expect(
			ui.confirm({ title: "Confirm", defaultValue: true }),
		).rejects.toThrow("interactivity is unavailable");
		expect(await ui.custom(() => undefined)).toBeUndefined();
	});
});
