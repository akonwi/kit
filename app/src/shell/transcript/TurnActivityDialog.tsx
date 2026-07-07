import type {
	OverlayComponentProps,
	OverlaySurfaceProps,
} from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { Dialog } from "../Dialog";
import { KeymapHintBar } from "../KeymapHintBar";
import { scrollbarStyle } from "../theme";
import {
	TURN_ACTIVITY_TITLE,
	turnActivityMetaText,
} from "./turn-activity-header";
import {
	type ActivitySource,
	createTurnActivityModel,
	TurnActivitySectionList,
} from "./turn-activity-view";

export type { ActivitySource } from "./turn-activity-view";

export type TurnActivityDialogProps = OverlayComponentProps<unknown> & {
	runtime: AgentRuntime;
	source: ActivitySource;
	surfaceProps?: OverlaySurfaceProps;
};

/**
 * Modal that presents the rich activity of a turn (or part of a turn) —
 * prose, tool calls with auto-expanded output, bash, handoffs.
 *
 * Reads live data directly from the runtime so updates flowing in while
 * the dialog is open are reflected without requiring close/reopen.
 */
export function TurnActivityDialog(props: TurnActivityDialogProps) {
	const model = createTurnActivityModel(props.runtime, props.source, () =>
		props.done(undefined),
	);

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active !== false,
		commands: {
			"turn-activity.close": () => props.done(undefined),
		},
	}));

	return (
		<Dialog.Root
			width="90%"
			maxWidth={160}
			height="85%"
			surfaceProps={props.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title>{TURN_ACTIVITY_TITLE}</Dialog.Title>
				<Dialog.Meta>{turnActivityMetaText(model)}</Dialog.Meta>
			</Dialog.Header>

			<Dialog.Body>
				{/* Sticky-bottom only for turns that were streaming when the
				 * dialog opened, so live activity follows automatically without
				 * yanking historical browse views to the bottom on open. */}
				<scrollbox
					flexGrow={1}
					scrollY
					stickyStart={model.initiallyLive ? "bottom" : undefined}
					stickyScroll={model.initiallyLive}
					style={scrollbarStyle()}
				>
					<TurnActivitySectionList model={model} />
				</scrollbox>
			</Dialog.Body>

			<Dialog.Footer>
				<KeymapHintBar borderless group="turn-activity" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
