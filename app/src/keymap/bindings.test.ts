import { describe, expect, test } from "bun:test";
import { Keymap, type KeymapEvent, type KeymapHost } from "@opentui/keymap";
import {
	registerDefaultKeys,
	registerMetadataFields,
} from "@opentui/keymap/addons";
import {
	type BindingDefinition,
	createConfiguredBindingResult,
	createConfiguredBindings,
	type KeybindingDiagnostic,
} from "./bindings";

function createTestKeymap(): Keymap<object, KeymapEvent> {
	return createDispatchableTestKeymap().keymap;
}

function createKeyEvent(
	input: Pick<KeymapEvent, "name"> & Partial<KeymapEvent>,
): KeymapEvent {
	let stopped = false;
	return {
		name: input.name,
		ctrl: input.ctrl ?? false,
		shift: input.shift ?? false,
		meta: input.meta ?? false,
		super: input.super,
		hyper: input.hyper,
		preventDefault: () => {},
		stopPropagation: () => {
			stopped = true;
		},
		get propagationStopped() {
			return stopped;
		},
	};
}

function createDispatchableTestKeymap(): {
	keymap: Keymap<object, KeymapEvent>;
	press: (event: Pick<KeymapEvent, "name"> & Partial<KeymapEvent>) => void;
} {
	const root = {};
	const keyListeners = new Set<(event: KeymapEvent) => void>();
	const host: KeymapHost<object, KeymapEvent> = {
		metadata: {
			platform: "unknown",
			primaryModifier: "unknown",
			modifiers: {
				ctrl: "supported",
				shift: "supported",
				meta: "unknown",
				super: "unknown",
				hyper: "unknown",
			},
		},
		rootTarget: root,
		isDestroyed: false,
		getFocusedTarget: () => root,
		getParentTarget: () => null,
		isTargetDestroyed: () => false,
		onKeyPress: (listener) => {
			keyListeners.add(listener);
			return () => keyListeners.delete(listener);
		},
		onKeyRelease: () => () => {},
		onFocusChange: () => () => {},
		onTargetDestroy: () => () => {},
		createCommandEvent: () => createKeyEvent({ name: "" }),
	};
	const keymap = new Keymap(host);
	registerDefaultKeys(keymap);
	registerMetadataFields(keymap);
	return {
		keymap,
		press: (event) => {
			const keyEvent = createKeyEvent(event);
			for (const listener of keyListeners) listener(keyEvent);
		},
	};
}

function collectBindings(
	definitions: readonly BindingDefinition[],
	settings?: Record<string, string | string[] | false | null>,
): { commands: (string | undefined)[]; diagnostics: KeybindingDiagnostic[] } {
	const result = createConfiguredBindingResult(
		createTestKeymap(),
		definitions,
		settings,
	);
	return {
		commands: result.bindings.map((binding) =>
			typeof binding.cmd === "string" ? binding.cmd : undefined,
		),
		diagnostics: result.diagnostics,
	};
}

describe("createConfiguredBindings", () => {
	test("keeps the first binding when two commands use the same key", () => {
		const result = collectBindings([
			{ cmd: "first", key: "ctrl+p", desc: "First" },
			{ cmd: "second", key: "ctrl+p", desc: "Second" },
		]);

		expect(result.commands).toEqual(["first"]);
		expect(result.diagnostics).toEqual([
			{
				type: "duplicate",
				command: "second",
				key: "ctrl+p",
				existingCommand: "first",
				existingKey: "ctrl+p",
			},
		]);
	});

	test("treats Kit aliases as same-layer conflicts", () => {
		const result = collectBindings([
			{ cmd: "submit", key: "return", desc: "Submit" },
			{ cmd: "also-submit", key: "enter", desc: "Submit again" },
		]);

		expect(result.commands).toEqual(["submit"]);
		expect(result.diagnostics).toEqual([
			{
				type: "duplicate",
				command: "also-submit",
				key: "enter",
				existingCommand: "submit",
				existingKey: "return",
			},
		]);
	});

	test("supports user overrides and disabled bindings", () => {
		const result = collectBindings(
			[
				{ cmd: "open", key: "ctrl+p", desc: "Open" },
				{ cmd: "close", key: "escape", desc: "Close" },
			],
			{ open: ["ctrl+space", "ctrl+o"], close: false },
		);

		expect(result.commands).toEqual(["open", "open"]);
		expect(result.diagnostics).toEqual([]);
	});

	test("reports invalid key strings and skips them", () => {
		const result = collectBindings([
			{ cmd: "bad", key: "ctrl+", desc: "Bad" },
			{ cmd: "good", key: "escape", desc: "Good" },
		]);

		expect(result.commands).toEqual(["good"]);
		expect(result.diagnostics).toHaveLength(1);
		expect(result.diagnostics[0]).toMatchObject({
			type: "invalid",
			command: "bad",
			key: "ctrl+",
		});
	});

	test("reports empty user key strings and skips them", () => {
		const result = collectBindings(
			[{ cmd: "empty", key: "escape", desc: "Empty" }],
			{ empty: ["", "   ", "ctrl+e"] },
		);

		expect(result.commands).toEqual(["empty"]);
		expect(result.diagnostics).toEqual([
			{
				type: "invalid",
				command: "empty",
				key: "",
				message: "Keybinding cannot be empty",
			},
			{
				type: "invalid",
				command: "empty",
				key: "   ",
				message: "Keybinding cannot be empty",
			},
		]);
	});

	test("allows cross-layer overlaps to use layer precedence", () => {
		const { keymap, press } = createDispatchableTestKeymap();
		const calls: string[] = [];

		keymap.registerLayer({
			priority: 1,
			commands: [
				{
					name: "low-priority",
					run: () => {
						calls.push("low-priority");
					},
				},
			],
			bindings: createConfiguredBindings(
				keymap,
				[{ cmd: "low-priority", key: "ctrl+p", desc: "Low" }],
				undefined,
			),
		});
		keymap.registerLayer({
			priority: 2,
			commands: [
				{
					name: "high-priority",
					run: () => {
						calls.push("high-priority");
					},
				},
			],
			bindings: createConfiguredBindings(
				keymap,
				[{ cmd: "high-priority", key: "ctrl+p", desc: "High" }],
				undefined,
			),
		});

		press({ name: "p", ctrl: true });

		expect(calls).toEqual(["high-priority"]);
	});
});
