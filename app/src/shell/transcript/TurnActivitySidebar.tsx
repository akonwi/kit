import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { KeymapHintBar } from "../KeymapHintBar";
import { theme } from "../theme";
import {
	type ActivitySource,
	createTurnActivityModel,
	TurnActivitySectionList,
} from "./turn-activity-view";

export type TurnActivitySidebarProps = {
	runtime: AgentRuntime;
	source: ActivitySource;
	onClose: () => void;
};

/**
 * Sidebar variant of the turn activity view. Mounts inline alongside the
 * transcript on wide viewports so users can keep reading the conversation
 * while inspecting tool activity.
 *
 * Shares the reactive model and section list with TurnActivityDialog, but
 * provides its own chrome (top header strip, left-border separator,
 * hint bar footer) and uses the "screen" keymap scope rather than
 * "modal" — the sidebar is part of the layout, not a floating overlay.
 */
export function TurnActivitySidebar(props: TurnActivitySidebarProps) {
	const model = createTurnActivityModel(
		props.runtime,
		props.source,
		props.onClose,
	);

	useKeymapLayer(() => ({
		scope: "panel",
		commands: {
			"turn-activity.close": props.onClose,
		},
	}));

	return (
		<box
			flexDirection="column"
			width="100%"
			height="100%"
			border={["left"]}
			borderColor={theme.borderDefault}
			backgroundColor={theme.bg}
		>
			<box
				flexShrink={0}
				flexDirection="row"
				justifyContent="space-between"
				paddingX={1}
				paddingY={0}
				borderColor={theme.borderDefault}
				border={["bottom"]}
			>
				<text fg={theme.textPrimary}>Turn activity</text>
				<text fg={theme.textMuted}>
					{model.toolCallCount()} tool call
					{model.toolCallCount() === 1 ? "" : "s"}
					{" · "}
					{model.stepCount()} step{model.stepCount() === 1 ? "" : "s"}
				</text>
			</box>

			<box flexGrow={1} flexDirection="column" paddingX={1} paddingY={1}>
				{/* Sticky-bottom only for turns that were streaming when the
				 * sidebar opened, so live activity follows automatically without
				 * snapping historical browse views to the bottom on open. */}
				<scrollbox
					flexGrow={1}
					scrollY
					stickyStart={model.initiallyLive ? "bottom" : undefined}
					stickyScroll={model.initiallyLive}
					style={{
						scrollbarOptions: {
							trackOptions: {
								foregroundColor: theme.scrollbarFg,
								backgroundColor: theme.scrollbarBg,
							},
						},
					}}
				>
					<TurnActivitySectionList model={model} />
				</scrollbox>
			</box>

			<box flexShrink={0}>
				<KeymapHintBar borderless group="turn-activity" />
			</box>
		</box>
	);
}
