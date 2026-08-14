import { describe, expect, test } from "bun:test";
import { mergePaletteCommands } from "./command-palette-commands";

describe("command palette commands", () => {
	test("adds browser adapters before remote commands", () => {
		expect(
			mergePaletteCommands([
				{ id: "compact", name: "compact", description: "Compact context" },
			]).map((command) => [command.id, command.browserAction]),
		).toEqual([
			["model", "model"],
			["thinking", "thinking"],
			["theme", "theme"],
			["toggle-scratchpad", "toggle-scratchpad"],
			["open-code-review", "open-code-review"],
			["compact", undefined],
		]);
	});

	test("keeps browser adapters canonical when remote ids collide", () => {
		const commands = mergePaletteCommands([
			{ id: "model", name: "remote model" },
			{ id: "thinking", name: "remote thinking" },
			{ id: "theme", name: "remote theme" },
			{ id: "toggle-scratchpad", name: "remote scratchpad" },
			{ id: "open-code-review", name: "remote review" },
		]);

		expect(commands.map((command) => command.name)).toEqual([
			"model",
			"thinking",
			"theme",
			"toggle-scratchpad",
			"code-review",
		]);
		expect(commands.every((command) => command.browserAction)).toBe(true);
	});
});
