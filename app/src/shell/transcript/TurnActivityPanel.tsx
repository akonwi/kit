import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { KeymapHintBar } from "../KeymapHintBar";
import { scrollbarStyle, theme } from "../theme";
import { WorkspacePanelLayout } from "../WorkspacePanelLayout";
import {
	TURN_ACTIVITY_TITLE,
	turnActivityMetaText,
} from "./turn-activity-header";
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
				<box
					flexShrink={0}
					flexDirection="row"
					justifyContent="space-between"
					paddingX={1}
					paddingY={0}
					borderColor={theme.borderDefault}
					border={["bottom"]}
				>
					<text fg={theme.textPrimary}>{TURN_ACTIVITY_TITLE}</text>
					<text fg={theme.textMuted}>{turnActivityMetaText(model)}</text>
				</box>
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
					style={scrollbarStyle()}
				>
					<TurnActivitySectionList model={model} />
				</scrollbox>
			</box>
		</WorkspacePanelLayout>
	);
}
