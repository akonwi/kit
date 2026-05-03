import type { KeyEvent } from "@opentui/core";
import { For, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
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
		? "✓"
		: state.status === "connecting"
			? "◌"
			: state.status === "error"
				? "✗"
				: state.status === "disabled"
					? "⊘"
					: "○";
}

export function McpStatusModal(props: McpStatusModalProps) {
	return (
		<box
			{...props.surfaceProps}
			position="absolute"
			left={0}
			top={0}
			width="100%"
			height="100%"
			justifyContent="center"
			alignItems="center"
			backgroundColor={theme.modalBackdrop}
			focusable
			focused
			onKeyDown={(event: KeyEvent) => {
				if (event.name === "escape" || event.name === "return") {
					event.preventDefault();
					props.onClose();
				}
			}}
		>
			<box
				width="78%"
				maxWidth={110}
				minWidth={56}
				height="75%"
				border
				borderStyle="double"
				borderColor={theme.borderFocused}
				backgroundColor={theme.bgSurface}
				padding={1}
				flexDirection="column"
				gap={1}
			>
				<box flexShrink={0} flexDirection="row" justifyContent="space-between">
					<text fg={theme.textPrimary}>MCP status</text>
					<text fg={theme.textMuted}>{props.states.length} configured</text>
				</box>

				<box
					flexGrow={1}
					border
					borderColor={theme.borderDefault}
					paddingX={1}
					paddingY={0}
					flexDirection="column"
				>
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
											<box
												border
												borderColor={theme.borderDefault}
												paddingX={1}
												flexDirection="column"
												gap={0}
											>
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
											{(warning) => (
												<text fg={theme.textMuted}>- {warning}</text>
											)}
										</For>
									</box>
								</Show>
							</box>
						</scrollbox>
					</Show>
				</box>

				<HintBar bindings={BINDINGS} />
			</box>
		</box>
	);
}
