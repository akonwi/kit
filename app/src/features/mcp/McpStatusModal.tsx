import type { Renderable } from "@opentui/core";
import { createSignal } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { McpStatusContent } from "./McpStatusContent";
import type { LoadMcpConfigResult, McpServerRuntimeState } from "./types";

export type McpStatusModalProps = {
	surfaceProps?: OverlaySurfaceProps;
	states: McpServerRuntimeState[];
	config: LoadMcpConfigResult | null;
	hasOAuthSession: (serverName: string) => boolean;
	active?: boolean;
	onClose: () => void;
};

export function McpStatusModal(props: McpStatusModalProps) {
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);

	useKeymapLayer(() => ({
		scope: "modal",
		target: rootTarget,
		targetMode: "focus-within",
		when: () => props.active !== false,
		commands: {
			"mcp-status.close": () => props.onClose(),
		},
	}));

	return (
		<Dialog.Root
			width="40%"
			height="50%"
			padding={0}
			gap={0}
			surfaceProps={props.surfaceProps}
			rootRef={setRootTarget}
			rootFocusable
			rootFocused={props.active !== false}
		>
			<Dialog.Header strip>
				<Dialog.Title>MCP status</Dialog.Title>
			</Dialog.Header>

			<Dialog.Body paddingBottom={1} overflow="hidden">
				<McpStatusContent
					states={props.states}
					config={props.config}
					hasOAuthSession={props.hasOAuthSession}
				/>
			</Dialog.Body>

			<Dialog.Footer strip>
				<KeymapHintBar borderless group="mcp-status" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
