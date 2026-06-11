import type { JSX } from "solid-js";
import { createMemo } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { ToolResultMessage } from "../../runtime/agent";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import { AssistantEntry } from "./assistant-entry";
import { BashEntry } from "./bash-entry";
import { DrawerChip } from "./drawer-chip";
import { HandoffSummaryEntry } from "./handoff-summary-entry";
import { TurnActivityDialog } from "./TurnActivityDialog";
import {
	type DisplayItem,
	extractAssistantParts,
	type TranscriptItem,
} from "./turns";
import type { OpenOverlay, TranscriptToast } from "./types";
import { UserEntry } from "./user-entry";

/**
 * Chip for the consolidated intermediate work of a turn. Clicking opens the
 * turn activity dialog, kept live via the runtime.
 */
function TurnWorkDrawer(props: {
	items: TranscriptItem[];
	liveTools: LiveToolsForTurn;
	runtime: AgentRuntime;
	openOverlay: OpenOverlay;
}) {
	if (props.items.length === 0) return null;

	const turnId = props.items[0].turnId;

	const allToolCalls = createMemo(() =>
		props.items.flatMap((item) =>
			item.kind === "assistant"
				? extractAssistantParts(item.message).toolCalls
				: [],
		),
	);
	const allToolResults = createMemo(() => {
		const merged = new Map<string, ToolResultMessage>();
		for (const item of props.items) {
			if (item.kind === "assistant") {
				for (const [id, result] of item.toolResults) {
					merged.set(id, result);
				}
			}
		}
		return merged;
	});
	const aborted = createMemo(() =>
		props.items.some((item) =>
			item.kind === "assistant" ? item.aborted : false,
		),
	);

	function openDialog() {
		const runtime = props.runtime;
		void props.openOverlay(
			(overlayProps: OverlayComponentProps<unknown>): JSX.Element => (
				<TurnActivityDialog
					runtime={runtime}
					source={{ kind: "turn-intermediate", turnId }}
					done={overlayProps.done}
					surfaceProps={overlayProps.surfaceProps}
					active={overlayProps.active}
				/>
			),
		);
	}

	const stepLabel = createMemo(() => {
		const n = props.items.length;
		return `${n} step${n === 1 ? "" : "s"}`;
	});

	return (
		<DrawerChip
			toolCalls={allToolCalls()}
			toolResults={allToolResults()}
			aborted={aborted()}
			onActivate={openDialog}
			emptyLabel={stepLabel()}
		/>
	);
}

export function TurnEntry(props: {
	displayItem: DisplayItem;
	liveTools: LiveToolsForTurn;
	showToast: (toast: TranscriptToast) => void;
	runtime: AgentRuntime;
	openOverlay: OpenOverlay;
}) {
	if (props.displayItem.kind === "turn-work") {
		return (
			<TurnWorkDrawer
				items={props.displayItem.items}
				liveTools={props.liveTools}
				runtime={props.runtime}
				openOverlay={props.openOverlay}
			/>
		);
	}

	const item = props.displayItem.item;
	switch (item.kind) {
		case "user":
			return (
				<UserEntry
					msg={item.message}
					aborted={item.aborted}
					showToast={props.showToast}
				/>
			);
		case "assistant":
			return (
				<AssistantEntry
					itemId={item.id}
					msg={item.message}
					toolResults={item.toolResults}
					liveTools={props.liveTools}
					aborted={item.aborted}
					runtime={props.runtime}
					openOverlay={props.openOverlay}
				/>
			);
		case "handoff-summary":
			return <HandoffSummaryEntry msg={item.message} aborted={item.aborted} />;
		case "bash":
			return <BashEntry msg={item.message} />;
	}
}
