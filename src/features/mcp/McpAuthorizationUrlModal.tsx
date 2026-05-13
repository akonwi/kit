import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { Dialog } from "../../shell/Dialog";
import { type Binding, HintBar } from "../../shell/HintBar";
import { theme } from "../../shell/theme";

const BINDINGS: Binding[] = [
	{ key: "Enter", action: "continue" },
	{ key: "Esc", action: "continue" },
];

export type McpAuthorizationUrlModalProps = {
	serverName: string;
	authorizationUrl: URL;
	active: boolean;
	surfaceProps?: OverlaySurfaceProps;
	onClose: () => void;
};

export function McpAuthorizationUrlModal(props: McpAuthorizationUrlModalProps) {
	useKeyboard((event: KeyEvent) => {
		if (!props.active) return;
		if (
			event.name === "return" ||
			event.name === "enter" ||
			event.name === "escape"
		) {
			event.preventDefault();
			props.onClose();
		}
	});

	const url = () => props.authorizationUrl.toString();

	return (
		<Dialog.Root
			surfaceProps={props.surfaceProps}
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
					<HintBar borderless bindings={BINDINGS} />
				</Dialog.Footer>
			</box>
		</Dialog.Root>
	);
}
