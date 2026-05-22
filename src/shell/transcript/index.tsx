import { createMemo, createSignal, onCleanup } from "solid-js";
import type { KitAgentMessage } from "../../session/types";
import { TranscriptPane } from "./pane";
import {
	buildAssistantTranscriptItem,
	buildBashTranscriptItem,
	buildUserTranscriptItem,
	extractAssistantParts,
	filterTranscriptItemsForDisplay,
	flattenTurnsToTranscriptItems,
	type TranscriptItem,
} from "./turns";
import type { TranscriptProps } from "./types";

function replaceTranscriptItem(
	prev: TranscriptItem[],
	item: TranscriptItem,
): TranscriptItem[] {
	let replaced = false;
	const next = prev.map((candidate) => {
		if (candidate.id !== item.id) return candidate;
		replaced = true;
		return item;
	});
	return replaced ? next : [...prev, item];
}

function finalizeBufferedAssistantMessage(
	bufferedText: string,
	message: Extract<KitAgentMessage, { role: "assistant" }>,
): Extract<KitAgentMessage, { role: "assistant" }> {
	if (bufferedText.length === 0) return message;
	return {
		...message,
		content: [
			{ type: "text", text: bufferedText },
			...message.content.filter((block) => block.type !== "text"),
		],
	};
}

export type { TranscriptProps } from "./types";

export function Transcript(props: TranscriptProps) {
	const [items, setItems] = createSignal(
		flattenTurnsToTranscriptItems(props.runtime.getTurns()),
	);
	const [pendingAssistantText, setPendingAssistantText] = createSignal("");
	const displayItems = createMemo(() =>
		filterTranscriptItemsForDisplay(items(), { zenMode: props.zenMode }),
	);

	const unsubscribeUserMessageCreated = props.runtime.subscribe(
		"user.message.created",
		(event) => {
			setItems((prev) => [...prev, buildUserTranscriptItem(event.message)]);
		},
	);

	const unsubscribeAgentMessageStarted = props.runtime.subscribe(
		"agent.message.started",
		(event) => {
			setPendingAssistantText(extractAssistantParts(event.message).text);
		},
	);

	const unsubscribeAgentMessageUpdated = props.runtime.subscribe(
		"agent.message.updated",
		(event) => {
			setPendingAssistantText(extractAssistantParts(event.message).text);
		},
	);

	const unsubscribeAgentMessageEnded = props.runtime.subscribe(
		"agent.message.ended",
		(event) => {
			setItems((prev) => [
				...prev,
				buildAssistantTranscriptItem(
					event.turn,
					finalizeBufferedAssistantMessage(
						pendingAssistantText(),
						event.message,
					),
				),
			]);
			setPendingAssistantText("");
		},
	);

	const unsubscribeBashCommandStarted = props.runtime.subscribe(
		"bash.command.started",
		(event) => {
			setItems((prev) => [...prev, buildBashTranscriptItem(event.message)]);
		},
	);

	const unsubscribeBashCommandCompleted = props.runtime.subscribe(
		"bash.command.completed",
		(event) => {
			setItems((prev) =>
				replaceTranscriptItem(prev, buildBashTranscriptItem(event.message)),
			);
		},
	);

	const unsubscribeTurnCompleted = props.runtime.subscribe(
		"agent.turn.completed",
		(_) => {
			setPendingAssistantText("");
			setItems(flattenTurnsToTranscriptItems(props.runtime.getTurns()));
		},
	);

	const unsubscribeSessionChanged = props.runtime.subscribe(
		"session.active.changed",
		(_) => {
			setPendingAssistantText("");
			setItems(flattenTurnsToTranscriptItems(props.runtime.getTurns()));
		},
	);

	const unsubscribeCompacted = props.runtime.subscribe(
		{ prefix: "session.compaction.completed" },
		(_) => {
			setItems(flattenTurnsToTranscriptItems(props.runtime.getTurns()));
		},
	);

	onCleanup(() => {
		unsubscribeUserMessageCreated();
		unsubscribeAgentMessageStarted();
		unsubscribeAgentMessageUpdated();
		unsubscribeAgentMessageEnded();
		unsubscribeBashCommandStarted();
		unsubscribeBashCommandCompleted();
		unsubscribeTurnCompleted();
		unsubscribeSessionChanged();
		unsubscribeCompacted();
	});

	return <TranscriptPane {...props} items={displayItems()} />;
}
