import type { LiveToolsForTurn } from "../transcript-live-tools";
import { AssistantEntry } from "./assistant-entry";
import { BashEntry } from "./bash-entry";
import { HandoffSummaryEntry } from "./handoff-summary-entry";
import type { TranscriptItem } from "./turns";
import type { TranscriptToast } from "./types";
import { UserEntry } from "./user-entry";

export function TurnEntry(props: {
	item: TranscriptItem;
	liveTools: LiveToolsForTurn;
	showToast: (toast: TranscriptToast) => void;
	zenMode?: boolean;
}) {
	switch (props.item.kind) {
		case "user":
			return (
				<UserEntry
					msg={props.item.message}
					aborted={props.item.aborted}
					showToast={props.showToast}
				/>
			);
		case "assistant":
			return (
				<AssistantEntry
					msg={props.item.message}
					toolResults={props.item.toolResults}
					liveTools={props.liveTools}
					aborted={props.item.aborted}
					zenMode={props.zenMode}
				/>
			);
		case "handoff-summary":
			return (
				<HandoffSummaryEntry
					msg={props.item.message}
					aborted={props.item.aborted}
				/>
			);
		case "bash":
			return <BashEntry msg={props.item.message} />;
	}
}
