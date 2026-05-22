import type {
	BindingInput,
	CommandDefinition,
	Keymap,
	KeymapEvent,
	KeySequencePart,
} from "@opentui/keymap";
import type { KeybindingSettings } from "../settings";
import { KIT_KEY_ALIASES } from "./setup";

export type BindingDefinition<TCommand extends string = string> = {
	cmd: TCommand;
	key: string | readonly string[];
	desc: string;
	group?: string;
};

export type CommandBindingDefinition<
	TTarget extends object = object,
	TEvent extends KeymapEvent = KeymapEvent,
	TCommand extends string = string,
> = {
	binding: BindingDefinition<TCommand>;
	command: {
		run: CommandDefinition<TTarget, TEvent>["run"];
		[key: string]: unknown;
	};
};

export type KeybindingDiagnostic =
	| {
			type: "unknown";
			command: string;
			message: string;
	  }
	| {
			type: "invalid";
			command: string;
			key: string;
			message: string;
	  }
	| {
			type: "duplicate";
			command: string;
			key: string;
			existingCommand: string;
			existingKey: string;
	  };

export type ConfiguredBindingsResult<
	TTarget extends object,
	TEvent extends KeymapEvent,
> = {
	bindings: BindingInput<TTarget, TEvent>[];
	diagnostics: KeybindingDiagnostic[];
};

function keysFromSetting<TCommand extends string>(
	definition: BindingDefinition<TCommand>,
	configured: KeybindingSettings[string] | undefined,
	diagnostics: KeybindingDiagnostic[],
): string[] {
	if (configured === false || configured === null) return [];
	const isConfigured = configured !== undefined;
	const source = configured ?? definition.key;
	const values = Array.isArray(source) ? source : [source];
	const keys: string[] = [];
	for (const value of values) {
		const key = value.trim();
		if (!key) {
			if (isConfigured) {
				diagnostics.push({
					type: "invalid",
					command: definition.cmd,
					key: value,
					message: "Keybinding cannot be empty",
				});
			}
			continue;
		}
		if (!keys.includes(key)) keys.push(key);
	}
	return keys;
}

function aliasKeyName(name: string): string {
	return KIT_KEY_ALIASES[name as keyof typeof KIT_KEY_ALIASES] ?? name;
}

function keyPartSignature(part: KeySequencePart): string {
	const stroke = part.stroke;
	return [
		aliasKeyName(stroke.name),
		stroke.ctrl ? "ctrl" : "",
		stroke.shift ? "shift" : "",
		stroke.meta ? "meta" : "",
		stroke.super ? "super" : "",
		stroke.hyper ? "hyper" : "",
	]
		.filter(Boolean)
		.join("+");
}

function bindingSignature<TTarget extends object, TEvent extends KeymapEvent>(
	keymap: Keymap<TTarget, TEvent>,
	key: string,
	command: string,
	diagnostics: KeybindingDiagnostic[],
): string | undefined {
	try {
		return keymap.parseKeySequence(key).map(keyPartSignature).join(" ");
	} catch (error) {
		diagnostics.push({
			type: "invalid",
			command,
			key,
			message: error instanceof Error ? error.message : String(error),
		});
		return undefined;
	}
}

export function createConfiguredBindingResult<
	TTarget extends object,
	TEvent extends KeymapEvent,
	TCommand extends string,
>(
	keymap: Keymap<TTarget, TEvent>,
	definitions: readonly BindingDefinition<TCommand>[],
	settings: KeybindingSettings | undefined,
): ConfiguredBindingsResult<TTarget, TEvent> {
	const bindings: BindingInput<TTarget, TEvent>[] = [];
	const diagnostics: KeybindingDiagnostic[] = [];
	const seen = new Map<string, { key: string; command: string }>();
	for (const definition of definitions) {
		const keys = keysFromSetting(
			definition,
			settings?.[definition.cmd],
			diagnostics,
		);
		for (const key of keys) {
			const signature = bindingSignature(
				keymap,
				key,
				definition.cmd,
				diagnostics,
			);
			if (!signature) continue;
			const existing = seen.get(signature);
			if (existing) {
				diagnostics.push({
					type: "duplicate",
					command: definition.cmd,
					key,
					existingCommand: existing.command,
					existingKey: existing.key,
				});
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
	return { bindings, diagnostics };
}

export function createConfiguredBindings<
	TTarget extends object,
	TEvent extends KeymapEvent,
	TCommand extends string,
>(
	keymap: Keymap<TTarget, TEvent>,
	definitions: readonly BindingDefinition<TCommand>[],
	settings: KeybindingSettings | undefined,
): BindingInput<TTarget, TEvent>[] {
	return createConfiguredBindingResult(keymap, definitions, settings).bindings;
}

export function createConfiguredCommandBindingResult<
	TTarget extends object,
	TEvent extends KeymapEvent,
	TCommand extends string,
>(
	keymap: Keymap<TTarget, TEvent>,
	definitions: readonly CommandBindingDefinition<TTarget, TEvent, TCommand>[],
	settings: KeybindingSettings | undefined,
): ConfiguredBindingsResult<TTarget, TEvent> {
	return createConfiguredBindingResult(
		keymap,
		definitions.map((definition) => definition.binding),
		settings,
	);
}

export function createKeymapCommands<
	TTarget extends object,
	TEvent extends KeymapEvent,
	TCommand extends string,
>(
	definitions: readonly CommandBindingDefinition<TTarget, TEvent, TCommand>[],
): CommandDefinition<TTarget, TEvent>[] {
	return definitions.map(({ binding, command }) => ({
		...command,
		desc: command.desc ?? binding.desc,
		group: command.group ?? binding.group,
		name: binding.cmd,
	}));
}

export function withKitKeyAliases<TLayer extends Record<string, unknown>>(
	layer: TLayer,
): TLayer & { aliases: typeof KIT_KEY_ALIASES } {
	return { ...layer, aliases: KIT_KEY_ALIASES };
}
