import { createMemo, Show } from "solid-js";
import type { ToolResultMessage } from "../../runtime/agent";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import { AssistantEntry, ToolDrawer } from "./assistant-entry";
import { BashEntry } from "./bash-entry";
import { HandoffSummaryEntry } from "./handoff-summary-entry";
import {
	type DisplayItem,
	extractAssistantParts,
	type TranscriptItem,
} from "./turns";
import type { TranscriptToast } from "./types";
import { UserEntry } from "./user-entry";

function ToolGroupEntry(props: {
	items: Extract<TranscriptItem, { kind: "assistant" }>[];
	liveTools: LiveToolsForTurn;
}) {
	// Defensive: groupItemsForDisplay never produces empty groups, but bail
	// out before creating memos if it ever does.
	if (props.items.length === 0) return null;

	// drawerId is derived from the first item id, which is stable as new
	// tool calls stream into the group.
	const drawerId = props.items[0].id;

	// Merge tool calls and results from all items in the group
	const allToolCalls = createMemo(() =>
		props.items.flatMap(
			(item) => extractAssistantParts(item.message).toolCalls,
		),
	);
	const allToolResults = createMemo(() => {
		const merged = new Map<string, ToolResultMessage>();
		for (const item of props.items) {
			for (const [id, result] of item.toolResults) {
				merged.set(id, result);
			}
		}
		return merged;
	});
	const aborted = createMemo(() => props.items.some((item) => item.aborted));

	return (
		<Show when={allToolCalls().length > 0}>
			<ToolDrawer
				drawerId={drawerId}
				toolCalls={allToolCalls()}
				toolResults={allToolResults()}
				liveTools={props.liveTools}
				aborted={aborted()}
			/>
		</Show>
	);
}

export function TurnEntry(props: {
	displayItem: DisplayItem;
	liveTools: LiveToolsForTurn;
	showToast: (toast: TranscriptToast) => void;
}) {
	if (props.displayItem.kind === "tool-group") {
		return (
			<ToolGroupEntry
				items={props.displayItem.items}
				liveTools={props.liveTools}
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
				/>
			);
		case "handoff-summary":
			return <HandoffSummaryEntry msg={item.message} aborted={item.aborted} />;
		case "bash":
			return <BashEntry msg={item.message} />;
	}
}
