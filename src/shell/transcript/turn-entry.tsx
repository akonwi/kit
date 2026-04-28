import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { AssistantMessage, ToolResultMessage } from "@mariozechner/pi-ai";
import { For, Show } from "solid-js";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import { AssistantEntry } from "./assistant-entry";
import { BashEntry } from "./bash-entry";
import { HandoffSummaryEntry } from "./handoff-summary-entry";
import {
	type BashExecutionMessage,
	isHandoffSummaryMessage,
	type TranscriptTurn,
} from "./turns";
import type { TranscriptToast } from "./types";
import { UserEntry } from "./user-entry";

function TurnEntryItem(props: {
	msg: AgentMessage;
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted: boolean;
}) {
	if (!("role" in props.msg)) return null;

	const role = props.msg.role as string;
	switch (role) {
		case "assistant":
			if (isHandoffSummaryMessage(props.msg)) {
				return <HandoffSummaryEntry msg={props.msg} aborted={props.aborted} />;
			}
			return (
				<AssistantEntry
					msg={props.msg as AssistantMessage}
					toolResults={props.toolResults}
					liveTools={props.liveTools}
					aborted={props.aborted}
				/>
			);
		case "bashExecution":
			return <BashEntry msg={props.msg as unknown as BashExecutionMessage} />;
		default:
			return null;
	}
}

export function TurnEntry(props: {
	turn: TranscriptTurn;
	liveTools: LiveToolsForTurn;
	showToast: (toast: TranscriptToast) => void;
}) {
	return (
		<box flexDirection="column" gap={1} width="100%">
			<Show when={props.turn.user}>
				{(user) => (
					<UserEntry
						msg={user()}
						aborted={props.turn.aborted}
						showToast={props.showToast}
					/>
				)}
			</Show>
			<For
				each={props.turn.entries.filter(
					(m) => "role" in m && m.role !== "toolResult",
				)}
			>
				{(msg) => (
					<TurnEntryItem
						msg={msg}
						toolResults={props.turn.toolResults}
						liveTools={props.liveTools}
						aborted={props.turn.aborted}
					/>
				)}
			</For>
		</box>
	);
}
