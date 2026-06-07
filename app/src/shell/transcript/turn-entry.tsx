import { createMemo, For } from "solid-js";
import type { ToolResultMessage } from "../../runtime/agent";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import { AssistantEntry, FlatAssistantEntry } from "./assistant-entry";
import { BashEntry } from "./bash-entry";
import { DrawerChip } from "./drawer-chip";
import { HandoffSummaryEntry } from "./handoff-summary-entry";
import {
	type DisplayItem,
	extractAssistantParts,
	type TranscriptItem,
} from "./turns";
import type { TranscriptToast } from "./types";
import { UserEntry } from "./user-entry";

/**
 * Renders the consolidated intermediate work of a turn — multiple assistant
 * messages, possibly mixed with bash items — as a single collapsible drawer.
 *
 * Expanded body renders the items inline in document order: assistant prose
 * + per-tool rows, bash entries, handoff summaries.
 */
function TurnWorkDrawer(props: {
	items: TranscriptItem[];
	liveTools: LiveToolsForTurn;
}) {
	if (props.items.length === 0) return null;

	const drawerId = `turn-work:${props.items[0].id}`;

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

	return (
		<DrawerChip
			drawerId={drawerId}
			toolCalls={allToolCalls()}
			toolResults={allToolResults()}
			aborted={aborted()}
		>
			<box paddingLeft={2} flexDirection="column" gap={1}>
				<For each={props.items}>
					{(item) => {
						if (item.kind === "assistant") {
							return (
								<FlatAssistantEntry
									msg={item.message}
									toolResults={item.toolResults}
									liveTools={props.liveTools}
									aborted={item.aborted}
								/>
							);
						}
						if (item.kind === "bash") {
							return <BashEntry msg={item.message} />;
						}
						if (item.kind === "handoff-summary") {
							return (
								<HandoffSummaryEntry
									msg={item.message}
									aborted={item.aborted}
								/>
							);
						}
						return null;
					}}
				</For>
			</box>
		</DrawerChip>
	);
}

export function TurnEntry(props: {
	displayItem: DisplayItem;
	liveTools: LiveToolsForTurn;
	showToast: (toast: TranscriptToast) => void;
}) {
	if (props.displayItem.kind === "turn-work") {
		return (
			<TurnWorkDrawer
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
