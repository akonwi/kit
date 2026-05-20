import type { BindingInput, Keymap, KeymapEvent } from "@opentui/keymap";
import type { KeybindingSettings } from "../settings";
import { KIT_KEY_ALIASES } from "./setup";

export type KitBindingDefinition<TCommand extends string = string> = {
	cmd: TCommand;
	key: string | readonly string[];
	desc: string;
	group?: string;
};

function keysFromSetting(
	configured: KeybindingSettings[string] | undefined,
	fallback: string | readonly string[],
): string[] {
	if (configured === false || configured === null) return [];
	const source = configured ?? fallback;
	const values = Array.isArray(source) ? source : [source];
	return values
		.map((key) => key.trim())
		.filter((key, index, all) => key.length > 0 && all.indexOf(key) === index);
}

function bindingSignature<TTarget extends object, TEvent extends KeymapEvent>(
	keymap: Keymap<TTarget, TEvent>,
	key: string,
	command: string,
): string | undefined {
	try {
		return keymap
			.parseKeySequence(key)
			.map((part) => part.match)
			.join(" ");
	} catch (error) {
		const message = error instanceof Error ? error.message : String(error);
		console.warn(
			`Ignoring invalid keybinding for ${command}: ${key} (${message})`,
		);
		return undefined;
	}
}

export function createConfiguredBindings<
	TTarget extends object,
	TEvent extends KeymapEvent,
	TCommand extends string,
>(
	keymap: Keymap<TTarget, TEvent>,
	definitions: readonly KitBindingDefinition<TCommand>[],
	settings: KeybindingSettings | undefined,
): BindingInput<TTarget, TEvent>[] {
	const bindings: BindingInput<TTarget, TEvent>[] = [];
	const seen = new Map<string, { key: string; command: string }>();
	for (const definition of definitions) {
		const keys = keysFromSetting(settings?.[definition.cmd], definition.key);
		for (const key of keys) {
			const signature = bindingSignature(keymap, key, definition.cmd);
			if (!signature) continue;
			const existing = seen.get(signature);
			if (existing) {
				console.warn(
					`Ignoring duplicate keybinding for ${definition.cmd}: ${key} already binds ${existing.command} as ${existing.key}`,
				);
				continue;
			}
			seen.set(signature, { key, command: definition.cmd });
			bindings.push({
				key,
				cmd: definition.cmd,
				desc: definition.desc,
				...(definition.group ? { group: definition.group } : {}),
			});
		}
	}
	return bindings;
}

export function withKitKeyAliases<TLayer extends Record<string, unknown>>(
	layer: TLayer,
): TLayer & { aliases: typeof KIT_KEY_ALIASES } {
	return { ...layer, aliases: KIT_KEY_ALIASES };
}
