import type { KeyEvent, Renderable } from "@opentui/core";
import type { TargetMode } from "@opentui/keymap";
import { useBindings, useKeymap } from "@opentui/keymap/solid";
import type { Accessor, JSX } from "solid-js";
import { createContext, createEffect, createMemo, useContext } from "solid-js";
import type { KeybindingSettings } from "../settings";
import {
	type CommandBindingDefinition,
	createConfiguredCommandBindingResult,
	createKeymapCommands,
	type KeybindingDiagnostic,
	withKitKeyAliases,
} from "./bindings";
import {
	type KeybindingDiagnosticReporter,
	reportKeybindingDiagnostics,
} from "./diagnostics";
import {
	type KeymapLayerPrecedence,
	type KeymapLayerScope,
	resolveKeymapLayerPriority,
} from "./precedence";
import {
	type BuiltInKeybindingCommandId,
	createCommandBindingDefinition,
	type OpenTuiCommandRun,
} from "./registry";

export type { OpenTuiCommandRun } from "./registry";

export type KeymapLayerCommandHandlers = Partial<
	Record<BuiltInKeybindingCommandId, OpenTuiCommandRun>
>;

type KeymapLayerContextValue = {
	keybindings: Accessor<KeybindingSettings | undefined>;
	onDiagnostic?: KeybindingDiagnosticReporter;
};

const KeymapLayerContext = createContext<KeymapLayerContextValue>();

export type KeymapLayerProviderProps = {
	keybindings: Accessor<KeybindingSettings | undefined>;
	onDiagnostic?: KeybindingDiagnosticReporter;
	children: JSX.Element;
};

export function KeymapLayerProvider(props: KeymapLayerProviderProps) {
	return (
		<KeymapLayerContext.Provider
			value={{
				keybindings: props.keybindings,
				onDiagnostic: props.onDiagnostic,
			}}
		>
			{props.children}
		</KeymapLayerContext.Provider>
	);
}

export type UseKeymapLayerOptions = {
	scope: KeymapLayerScope;
	precedence?: KeymapLayerPrecedence;
	when?: () => boolean;
	target?: () => Renderable | null | undefined;
	targetMode?: TargetMode;
	commands: KeymapLayerCommandHandlers;
};

type KeymapLayerDefinitionsResult = {
	definitions: CommandBindingDefinition<Renderable, KeyEvent>[];
	diagnostics: KeybindingDiagnostic[];
};

function diagnosticsSignature(value: unknown): string {
	return JSON.stringify(value);
}

/**
 * Registers a Kit keymap layer from static keybinding command ids.
 *
 * Must be called under `KeymapLayerProvider`; the provider supplies user
 * keybinding settings and the centralized diagnostic reporter. Unknown command
 * ids are reported as diagnostics and skipped rather than throwing during render.
 */
export function useKeymapLayer(createLayer: () => UseKeymapLayerOptions): void {
	const keymap = useKeymap();
	const context = useContext(KeymapLayerContext);
	if (!context) {
		throw new Error("useKeymapLayer must be used inside KeymapLayerProvider");
	}
	const layerOptions = createMemo(createLayer);
	const definitionsResult = createMemo<KeymapLayerDefinitionsResult>(() => {
		const definitions: CommandBindingDefinition<Renderable, KeyEvent>[] = [];
		const diagnostics: KeybindingDiagnostic[] = [];
		for (const [id, run] of Object.entries(layerOptions().commands)) {
			try {
				definitions.push(createCommandBindingDefinition(id, run));
			} catch (error) {
				diagnostics.push({
					type: "unknown",
					command: id,
					message: error instanceof Error ? error.message : String(error),
				});
			}
		}
		return { definitions, diagnostics };
	});
	const bindingResult = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			definitionsResult().definitions,
			context.keybindings(),
		),
	);
	let lastDiagnosticsSignature = "";

	createEffect(() => {
		const diagnostics = [
			...definitionsResult().diagnostics,
			...bindingResult().diagnostics,
		];
		const signature = diagnosticsSignature(diagnostics);
		if (signature === lastDiagnosticsSignature) return;
		lastDiagnosticsSignature = signature;
		reportKeybindingDiagnostics(diagnostics, context.onDiagnostic);
	});

	useBindings(() => {
		const layer = layerOptions();
		const baseLayer = {
			enabled: layer.when,
			priority: resolveKeymapLayerPriority(layer.scope, layer.precedence),
			commands: createKeymapCommands(definitionsResult().definitions),
			bindings: bindingResult().bindings,
		};
		if (!layer.target) return withKitKeyAliases(baseLayer);
		return withKitKeyAliases({
			...baseLayer,
			target: layer.target,
			targetMode: layer.targetMode,
		});
	});
}
