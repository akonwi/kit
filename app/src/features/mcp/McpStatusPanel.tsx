import { createEffect, createSignal, onCleanup } from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { WorkspacePanelLayout } from "../../shell/WorkspacePanelLayout";
import { McpStatusContent } from "./McpStatusContent";
import type { McpPanelData } from "./workspace-controller";

export type McpStatusPanelProps = {
	data: () => McpPanelData | null;
	active?: boolean;
	onClose: () => void;
	onFocusRequest?: () => void;
};

export function McpStatusPanel(props: McpStatusPanelProps) {
	const [revision, setRevision] = createSignal(0);
	const active = () => props.active !== false;
	const currentData = () => {
		revision();
		return props.data();
	};

	createEffect(() => {
		const data = props.data();
		if (!data) return;
		onCleanup(
			data.subscribeToChanges(() => setRevision((current) => current + 1)),
		);
	});

	useKeymapLayer(() => ({
		scope: "panel",
		when: active,
		commands: {
			"mcp-status.close": props.onClose,
		},
	}));

	return (
		<box
			width="100%"
			height="100%"
			onMouseDown={(event) => {
				if (event.button === 0) props.onFocusRequest?.();
			}}
		>
			<WorkspacePanelLayout
				footer={<KeymapHintBar borderless group="mcp-status" />}
			>
				<McpStatusContent
					states={currentData()?.getStates() ?? []}
					config={currentData()?.getConfig() ?? null}
					hasOAuthSession={(serverName) =>
						currentData()?.hasOAuthSession(serverName) ?? false
					}
				/>
			</WorkspacePanelLayout>
		</box>
	);
}
