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

export type McpAuthorizationUrlModalProps = {
	serverName: string;
	authorizationUrl: URL;
	active: boolean;
	surfaceProps?: OverlaySurfaceProps;
	settings?: Settings;
	onKeybindingDiagnostic?: (diagnostic: KeybindingDiagnostic) => void;
	onClose: () => void;
};

export function McpAuthorizationUrlModal(props: McpAuthorizationUrlModalProps) {
	const keymap = useKeymap();
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);
	const commands = [
		{
			binding: {
				cmd: "mcp-authorization-url.continue",
				key: ["return", "escape"],
				desc: "Continue after MCP authorization",
				group: "mcp-authorization-url",
			},
			command: {
				hint: "continue",
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
			enabled: () => props.active,
			priority: 200,
			commands: createKeymapCommands(commands),
			bindings: bindings().bindings,
		}),
	);

	const url = () => props.authorizationUrl.toString();

	return (
		<Dialog.Root
			surfaceProps={props.surfaceProps}
			rootRef={setRootTarget}
			rootFocusable
			rootFocused={props.active}
			width="80%"
			maxWidth={100}
			minWidth={50}
			height="50%"
			padding={0}
		>
			<box flexGrow={1} flexDirection="column" paddingX={1} gap={1}>
				<Dialog.Header>
					<Dialog.Title>Open MCP authorization URL</Dialog.Title>
				</Dialog.Header>
				<text fg={theme.textMuted}>
					{`Kit could not open a browser for ${props.serverName}. Open this URL manually, then press Enter to continue.`}
				</text>
				<scrollbox
					flexGrow={1}
					scrollY
					style={{
						scrollbarOptions: {
							trackOptions: {
								foregroundColor: theme.scrollbarFg,
								backgroundColor: theme.scrollbarBg,
							},
						},
					}}
				>
					<box width="100%" backgroundColor={theme.bg} paddingX={1}>
						<text fg={theme.textSecondary}>{url()}</text>
					</box>
				</scrollbox>
				<Dialog.Footer paddingY={1}>
					<KeymapHintBar borderless group="mcp-authorization-url" />
				</Dialog.Footer>
			</box>
		</Dialog.Root>
	);
}
