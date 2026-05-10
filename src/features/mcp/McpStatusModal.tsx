import { useKeyboard } from "@opentui/solid";
import { For, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { Dialog } from "../../shell/Dialog";
import { CHECK, CIRCLE_EMPTY, CIRCLE_SLASH, CROSS } from "../../shell/glyphs";
import { type Binding, HintBar } from "../../shell/HintBar";
import { theme } from "../../shell/theme";
import type { LoadMcpConfigResult, McpServerRuntimeState } from "./types";

export type McpStatusModalProps = {
	surfaceProps?: OverlaySurfaceProps;
	states: McpServerRuntimeState[];
	config: LoadMcpConfigResult | null;
	hasOAuthSession: (serverName: string) => boolean;
	onClose: () => void;
};

const BINDINGS: Binding[] = [{ key: "Esc/Enter", action: "close" }];

function statusPrefix(state: McpServerRuntimeState): string {
	return state.status === "connected"
		? CHECK
		: state.status === "connecting"
			? CIRCLE_EMPTY
			: state.status === "error"
				? CROSS
				: state.status === "disabled"
					? CIRCLE_SLASH
					: CIRCLE_EMPTY;
}

export function McpStatusModal(props: McpStatusModalProps) {
	useKeyboard((e) => {
		if (e.name === "escape" || e.name === "return") {
			e.preventDefault();
			props.onClose();
		}
	});

	return (
		<Dialog.Root
			width="78%"
			maxWidth={110}
			minWidth={56}
			height="75%"
			surfaceProps={props.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title>MCP status</Dialog.Title>
				<Dialog.Meta>{props.states.length} configured</Dialog.Meta>
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
												<text fg={theme.textPrimary}>
													{statusPrefix(state)} {state.name}
												</text>
												<text fg={theme.textMuted}>{state.type}</text>
											</box>
											<text fg={theme.textSecondary}>
												status: {state.status}
												{state.toolCount > 0
													? ` · ${state.toolCount} tools`
													: ""}
												{state.cached ? " · cached" : ""}
												{oauth() ? " · oauth saved" : ""}
											</text>
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
				<HintBar bindings={BINDINGS} />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
