import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { KeymapHintBar } from "../KeymapHintBar";
import { scrollbarStyle, theme } from "../theme";
import {
	WorkspacePanelHeader,
	WorkspacePanelLayout,
} from "../WorkspacePanelLayout";
import { turnActivityMetaText } from "./turn-activity-header";
import {
	type ActivitySource,
	createTurnActivityModel,
	TurnActivitySectionList,
} from "./turn-activity-view";

export type TurnActivityPanelProps = {
	runtime: AgentRuntime;
	source: ActivitySource;
	active: boolean;
	onClose: () => void;
	onFocusRequest: () => void;
};

/** Persistent workspace-panel presentation for turn activity details. */
export function TurnActivityPanel(props: TurnActivityPanelProps) {
	const model = createTurnActivityModel(
		props.runtime,
		props.source,
		props.onClose,
	);

	useKeymapLayer(() => ({
		scope: "panel",
		when: () => props.active,
		commands: {
			"turn-activity.close": props.onClose,
		},
	}));

	return (
		<WorkspacePanelLayout
			header={
				<WorkspacePanelHeader
					left={<text fg={theme.textMuted}>{turnActivityMetaText(model)}</text>}
				/>
			}
			footer={<KeymapHintBar borderless group="turn-activity" />}
		>
			<box
				flexGrow={1}
				flexDirection="column"
				paddingX={1}
				paddingY={1}
				onMouseDown={(event) => {
					if (event.button === 0) props.onFocusRequest();
				}}
			>
				{/* Sticky-bottom only for turns that were streaming when the panel
				 * opened, so historical activity still opens at the top. */}
				<scrollbox
					flexGrow={1}
					scrollY
					stickyStart={model.initiallyLive ? "bottom" : undefined}
					stickyScroll={model.initiallyLive}
					contentOptions={{ flexDirection: "column", gap: 0, width: "100%" }}
					style={scrollbarStyle()}
				>
					<TurnActivitySectionList model={model} />
				</scrollbox>
			</box>
		</WorkspacePanelLayout>
	);
}
