import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { KeymapHintBar } from "../KeymapHintBar";
import { scrollbarStyle, theme } from "../theme";
import {
	TURN_ACTIVITY_TITLE,
	turnActivityMetaText,
} from "./turn-activity-header";
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
				<text fg={theme.textPrimary}>{TURN_ACTIVITY_TITLE}</text>
				<text fg={theme.textMuted}>{turnActivityMetaText(model)}</text>
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
					style={scrollbarStyle()}
				>
					<TurnActivitySectionList model={model} />
				</scrollbox>
			</box>

			{/* Bordered (default — i.e. no `borderless`) because the hint bar
			 * is the outermost structural element at the bottom of the inline
			 * panel — see design SKILL.md (Hint Bar). The modal variant uses
			 * `borderless` since Dialog.Root already frames it. */}
			<box flexShrink={0}>
				<KeymapHintBar group="turn-activity" />
			</box>
		</box>
	);
}
