import { describe, expect, test } from "bun:test";
import { createPickerCommandMetadata } from "./registry";
import type {
	GeneratedKeymapLayerCommandHandlers,
	KeymapLayerCommandHandlers,
} from "./useKeymapLayer";

describe("keybinding registry", () => {
	test("creates namespaced picker command metadata", () => {
		const metadata = createPickerCommandMetadata("command-palette", {
			includeComplete: true,
			selectHint: "run",
		});

		expect(Object.keys(metadata).sort()).toEqual([
			"command-palette.close",
			"command-palette.complete",
			"command-palette.move-down",
			"command-palette.move-up",
			"command-palette.select",
		]);
		expect(metadata["command-palette.move-up"]).toMatchObject({
			defaultKeys: "up",
			desc: "Move picker selection up",
			group: "command-palette",
			hint: "up",
		});
		expect(metadata["command-palette.move-down"]).toMatchObject({
			defaultKeys: "down",
			group: "command-palette",
			hint: "down",
		});
		expect(metadata["command-palette.select"]).toMatchObject({
			defaultKeys: "return",
			group: "command-palette",
			hint: "run",
		});
		expect(metadata["command-palette.close"]).toMatchObject({
			defaultKeys: "escape",
			group: "command-palette",
			hint: "close",
		});
		expect(metadata["command-palette.complete"]).toMatchObject({
			defaultKeys: "tab",
			group: "command-palette",
			hint: "complete",
		});
	});

	test("omits picker completion metadata unless requested", () => {
		const metadata = createPickerCommandMetadata("picker");

		expect(Object.keys(metadata).sort()).toEqual([
			"picker.close",
			"picker.move-down",
			"picker.move-up",
			"picker.select",
		]);
		expect(metadata["picker.complete"]).toBeUndefined();
		expect(metadata["picker.select"]).toMatchObject({
			defaultKeys: "return",
			group: "picker",
			hint: "select",
		});
	});

	test("keeps generated picker commands out of built-in handler type", () => {
		const builtInHandlers: KeymapLayerCommandHandlers = {
			"command-palette.open": () => undefined,
		};
		const generatedHandlers: GeneratedKeymapLayerCommandHandlers = {
			"picker.select": () => undefined,
		};
		const invalidBuiltInHandlers: KeymapLayerCommandHandlers = {
			// @ts-expect-error generated picker commands must use generatedCommands.
			"picker.select": () => undefined,
		};

		expect(Object.keys(builtInHandlers)).toEqual(["command-palette.open"]);
		expect(Object.keys(generatedHandlers)).toEqual(["picker.select"]);
		expect(Object.keys(invalidBuiltInHandlers)).toEqual(["picker.select"]);
	});
});
