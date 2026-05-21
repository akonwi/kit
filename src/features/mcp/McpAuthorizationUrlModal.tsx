import type { Renderable } from "@opentui/core";
import { createSignal } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";

export type McpAuthorizationUrlModalProps = {
	serverName: string;
	authorizationUrl: URL;
	active: boolean;
	surfaceProps?: OverlaySurfaceProps;
	onClose: () => void;
};

export function McpAuthorizationUrlModal(props: McpAuthorizationUrlModalProps) {
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);

	useKeymapLayer(() => ({
		scope: "modal",
		target: rootTarget,
		targetMode: "focus-within",
		when: () => props.active,
		commands: {
			"mcp-authorization-url.continue": () => props.onClose(),
		},
	}));

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
