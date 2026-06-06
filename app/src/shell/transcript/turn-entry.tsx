import { Show } from "solid-js";
import type { ToolCall, ToolResultMessage } from "../../runtime/agent";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import {
	AssistantEntry,
	CompletedToolSummary,
	InProgressToolCalls,
} from "./assistant-entry";
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
	zenMode?: boolean;
}) {
	// Merge tool calls and results from all items in the group
	const allToolCalls = (): ToolCall[] =>
		props.items.flatMap(
			(item) => extractAssistantParts(item.message).toolCalls,
		);
	const allToolResults = (): Map<string, ToolResultMessage> => {
		const merged = new Map<string, ToolResultMessage>();
		for (const item of props.items) {
			for (const [id, result] of item.toolResults) {
				merged.set(id, result);
			}
		}
		return merged;
	};
	const aborted = () => props.items.some((item) => item.aborted);

	const allCompleted = () => {
		const tcs = allToolCalls();
		const results = allToolResults();
		return tcs.length > 0 && tcs.every((tc) => results.has(tc.id));
	};

	return (
		<Show when={!props.zenMode}>
			<Show
				when={allCompleted()}
				fallback={
					<InProgressToolCalls
						toolCalls={allToolCalls()}
						toolResults={allToolResults()}
						liveTools={props.liveTools}
						aborted={aborted()}
					/>
				}
			>
				<CompletedToolSummary
					toolCalls={allToolCalls()}
					toolResults={allToolResults()}
					aborted={aborted()}
				/>
			</Show>
		</Show>
	);
}

export function TurnEntry(props: {
	displayItem: DisplayItem;
	liveTools: LiveToolsForTurn;
	showToast: (toast: TranscriptToast) => void;
	zenMode?: boolean;
}) {
	if (props.displayItem.kind === "tool-group") {
		return (
			<ToolGroupEntry
				items={props.displayItem.items}
				liveTools={props.liveTools}
				zenMode={props.zenMode}
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
					msg={item.message}
					toolResults={item.toolResults}
					liveTools={props.liveTools}
					aborted={item.aborted}
					zenMode={props.zenMode}
				/>
			);
		case "handoff-summary":
			return <HandoffSummaryEntry msg={item.message} aborted={item.aborted} />;
		case "bash":
			return <BashEntry msg={item.message} />;
	}
}
