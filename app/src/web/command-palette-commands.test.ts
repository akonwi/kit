import { describe, expect, test } from "bun:test";
import { mergePaletteCommands } from "./command-palette-commands";

describe("command palette commands", () => {
	test("adds browser model controls before remote commands", () => {
		expect(
			mergePaletteCommands([
				{ id: "compact", name: "compact", description: "Compact context" },
			]).map((command) => [command.id, command.browserAction]),
		).toEqual([
			["model", "model"],
			["thinking", "thinking"],
			["compact", undefined],
		]);
	});

	test("keeps browser adapters canonical when remote ids collide", () => {
		const commands = mergePaletteCommands([
			{ id: "model", name: "remote model" },
			{ id: "thinking", name: "remote thinking" },
		]);

		expect(commands.map((command) => command.name)).toEqual([
			"model",
			"thinking",
		]);
		expect(commands.every((command) => command.browserAction)).toBe(true);
	});
});
