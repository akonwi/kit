import type { Renderable } from "@opentui/core";
import { useBindings, useKeymap } from "@opentui/keymap/solid";
import { createEffect, createMemo, createSignal } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import {
	type CommandBindingDefinition,
	createConfiguredCommandBindingResult,
	createKeymapCommands,
	type KeybindingDiagnostic,
	withKitKeyAliases,
} from "../../keymap/bindings";
import { reportKeybindingDiagnostics } from "../../keymap/diagnostics";
import type { Settings } from "../../settings";
import { Dialog } from "../../shell/Dialog";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";

export type DebugModalProps = {
	title: string;
	lines: string[];
	settings?: Settings;
	active?: boolean;
	surfaceProps?: OverlaySurfaceProps;
	onKeybindingDiagnostic?: (diagnostic: KeybindingDiagnostic) => void;
	onClose: () => void;
};

export function DebugModal(props: DebugModalProps) {
	const keymap = useKeymap();
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);
	const commands = [
		{
			binding: {
				cmd: "debug.close",
				key: ["return", "escape"],
				desc: "Close debug view",
				group: "debug",
			},
			command: {
				hint: "close",
				run: () => props.onClose(),
			},
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const bindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			commands,
			props.settings?.keybindings,
		),
	);

	createEffect(() => {
		reportKeybindingDiagnostics(
			bindings().diagnostics,
			props.onKeybindingDiagnostic,
		);
	});

	useBindings(() =>
		withKitKeyAliases({
			target: rootTarget,
			targetMode: "focus-within",
			enabled: () => props.active !== false,
			priority: 200,
			commands: createKeymapCommands(commands),
			bindings: bindings().bindings,
		}),
	);

	return (
		<Dialog.Root
			height="70%"
			surfaceProps={props.surfaceProps}
			rootRef={setRootTarget}
		>
			<Dialog.Header>
				<Dialog.Title>{props.title}</Dialog.Title>
			</Dialog.Header>
			<Dialog.Body>
				<scrollbox flexGrow={1} scrollY focused>
					<box flexDirection="column" gap={0} width="100%">
						{props.lines.map((line) => (
							<text fg={theme.textSecondary}>{line}</text>
						))}
					</box>
				</scrollbox>
			</Dialog.Body>
			<Dialog.Footer paddingBottom={1}>
				<KeymapHintBar borderless group="debug" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
