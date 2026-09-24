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
	type KeybindingCommandMetadata,
	type KeybindingCommandMetadataMap,
	type OpenTuiCommandRun,
} from "./registry";

export type { OpenTuiCommandRun } from "./registry";

export type KeymapLayerCommandHandlers = Partial<
	Record<BuiltInKeybindingCommandId, OpenTuiCommandRun>
>;

export type GeneratedKeymapLayerCommandHandlers = Record<
	string,
	OpenTuiCommandRun | undefined
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
	diagnosticsWhen?: () => boolean;
	target?: () => Renderable | null | undefined;
	targetMode?: TargetMode;
	/**
	 * Metadata for generated command ids that are not part of the static built-in
	 * registry. Each generated command id must have a matching entry here so the
	 * layer can derive default keys, descriptions, groups, and hint labels.
	 */
	commandMetadata?: KeybindingCommandMetadataMap;
	/** Statically registered Kit command handlers. */
	commands: KeymapLayerCommandHandlers;
	/** Dynamic or namespaced command handlers, such as picker instance commands. */
	generatedCommands?: GeneratedKeymapLayerCommandHandlers;
};

type KeymapLayerDefinitionsResult = {
	definitions: CommandBindingDefinition<Renderable, KeyEvent>[];
	diagnostics: KeybindingDiagnostic[];
};

function diagnosticsSignature(value: unknown): string {
	return JSON.stringify(value);
}

function createUnknownCommandDiagnostic(
	id: string,
	message: string,
): KeybindingDiagnostic {
	return {
		type: "unknown",
		command: id,
		message,
	};
}

function appendCommandDefinition(
	definitions: CommandBindingDefinition<Renderable, KeyEvent>[],
	diagnostics: KeybindingDiagnostic[],
	id: string,
	run: OpenTuiCommandRun | undefined,
	metadata?: KeybindingCommandMetadata,
	requireMetadata = false,
): void {
	if (!run) return;
	if (requireMetadata && !metadata) {
		diagnostics.push(
			createUnknownCommandDiagnostic(
				id,
				`Missing generated command metadata: ${id}`,
			),
		);
		return;
	}
	try {
		definitions.push(createCommandBindingDefinition(id, run, metadata));
	} catch (error) {
		diagnostics.push(
			createUnknownCommandDiagnostic(
				id,
				error instanceof Error ? error.message : String(error),
			),
		);
	}
}

/**
 * Registers a Kit keymap layer from registry-backed command ids.
 *
 * Must be called under `KeymapLayerProvider`; the provider supplies user
 * keybinding settings and the centralized diagnostic reporter. Use
 * `diagnosticsWhen` for mutually exclusive layers that reuse command ids so
 * inactive variants do not repeat the same settings warning.
 *
 * `commands` only accepts statically registered built-in command ids.
 * Namespaced/generated command ids must be passed through `generatedCommands`
 * with matching `commandMetadata`; a generated command without metadata is
 * reported and skipped. Unknown command ids are reported as diagnostics and
 * skipped rather than throwing during render.
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
		const layer = layerOptions();
		for (const [id, run] of Object.entries(layer.commands)) {
			appendCommandDefinition(definitions, diagnostics, id, run);
		}
		for (const [id, run] of Object.entries(layer.generatedCommands ?? {})) {
			appendCommandDefinition(
				definitions,
				diagnostics,
				id,
				run,
				layer.commandMetadata?.[id],
				true,
			);
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
		const diagnosticsWhen = layerOptions().diagnosticsWhen;
		if (diagnosticsWhen && !diagnosticsWhen()) return;
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
