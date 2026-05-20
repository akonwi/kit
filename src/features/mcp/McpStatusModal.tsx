import type { Renderable } from "@opentui/core";
import { useBindings, useKeymap } from "@opentui/keymap/solid";
import { createEffect, createMemo, createSignal, For, Show } from "solid-js";
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
import { CHECK, CIRCLE_EMPTY, CIRCLE_SLASH, CROSS } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { Spinner } from "../../shell/Spinner";
import { theme } from "../../shell/theme";
import type { LoadMcpConfigResult, McpServerRuntimeState } from "./types";

export type McpStatusModalProps = {
	surfaceProps?: OverlaySurfaceProps;
	states: McpServerRuntimeState[];
	config: LoadMcpConfigResult | null;
	hasOAuthSession: (serverName: string) => boolean;
	settings?: Settings;
	onKeybindingDiagnostic?: (diagnostic: KeybindingDiagnostic) => void;
	active?: boolean;
	onClose: () => void;
};

function statusIndicator(state: McpServerRuntimeState): {
	glyph: string | null;
	color: string;
	spinning: boolean;
} {
	switch (state.status) {
		case "connected":
			return { glyph: CHECK, color: theme.toolText, spinning: false };
		case "connecting":
			return { glyph: null, color: theme.toolText, spinning: true };
		case "error":
			return { glyph: CROSS, color: theme.errorText, spinning: false };
		case "disabled":
			return { glyph: CIRCLE_SLASH, color: theme.textMuted, spinning: false };
		default:
			return { glyph: CIRCLE_EMPTY, color: theme.textMuted, spinning: false };
	}
}

export function McpStatusModal(props: McpStatusModalProps) {
	const keymap = useKeymap();
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);
	const commands = [
		{
			binding: {
				cmd: "mcp-status.close",
				key: ["escape", "return"],
				desc: "Close MCP status",
				group: "mcp-status",
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
			width="40%"
			height="50%"
			surfaceProps={props.surfaceProps}
			rootRef={setRootTarget}
			rootFocusable
			rootFocused={props.active !== false}
		>
			<Dialog.Header>
				<Dialog.Title>MCP status</Dialog.Title>
			</Dialog.Header>

			<Dialog.Body>
				<Show
					when={props.states.length > 0}
					fallback={
						<box flexGrow={1} justifyContent="center" alignItems="center">
							<text fg={theme.textMuted}>No MCP servers are configured.</text>
						</box>
					}
				>
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
						<box flexDirection="column" gap={1} width="100%">
							<For each={props.states}>
								{(state) => {
									const oauth = () => props.hasOAuthSession(state.name);
									return (
										<box flexDirection="column" gap={0}>
											<box flexDirection="row" justifyContent="space-between">
												<box flexDirection="row" gap={1}>
													<Show
														when={!statusIndicator(state).spinning}
														fallback={
															<Spinner fg={statusIndicator(state).color} />
														}
													>
														<text fg={statusIndicator(state).color}>
															{statusIndicator(state).glyph}
														</text>
													</Show>
													<text fg={theme.textPrimary}>{state.name}</text>
												</box>
												<text fg={theme.textMuted}>{state.type}</text>
											</box>
											<Show
												when={state.toolCount > 0 || state.cached || oauth()}
											>
												<text fg={theme.textSecondary}>
													{[
														state.toolCount > 0 && `${state.toolCount} tools`,
														state.cached && "cached",
														oauth() && "oauth saved",
													]
														.filter(Boolean)
														.join(" · ")}
												</text>
											</Show>
											<Show when={state.description}>
												<text fg={theme.textSecondary}>
													{state.description}
												</text>
											</Show>
											<text fg={theme.textMuted}>
												{state.source} · {state.filePath}
											</text>
											<Show when={state.lastError}>
												<text fg={theme.errorText}>
													error: {state.lastError}
												</text>
											</Show>
										</box>
									);
								}}
							</For>

							<Show when={(props.config?.warnings.length ?? 0) > 0}>
								<box flexDirection="column" gap={0}>
									<text fg={theme.warningText}>Warnings</text>
									<For each={props.config?.warnings ?? []}>
										{(warning) => <text fg={theme.textMuted}>- {warning}</text>}
									</For>
								</box>
							</Show>
						</box>
					</scrollbox>
				</Show>
			</Dialog.Body>

			<Dialog.Footer>
				<KeymapHintBar borderless group="mcp-status" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
