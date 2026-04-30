import { createSignal, onCleanup } from "solid-js";
import type { KitAgentMessage, Turn } from "../../session/types";
import { TranscriptPane } from "./pane";
import { extractAssistantParts } from "./turns";
import type { TranscriptProps } from "./types";

function appendTurnMessage(
	prev: Turn[],
	turnId: string,
	message: KitAgentMessage,
): Turn[] {
	return prev.map((turn) =>
		turn.id === turnId
			? { ...turn, messages: [...turn.messages, message] }
			: turn,
	);
}

function appendOrCreateTurnMessage(
	prev: Turn[],
	turnId: string,
	message: KitAgentMessage,
): Turn[] {
	const hasTurn = prev.some((turn) => turn.id === turnId);
	if (!hasTurn) {
		return [...prev, { id: turnId, messages: [message] }];
	}
	return appendTurnMessage(prev, turnId, message);
}

function replaceTurnMessage(
	prev: Turn[],
	turnId: string,
	message: KitAgentMessage,
): Turn[] {
	let replaced = false;
	const next = prev.map((turn) =>
		turn.id === turnId
			? {
					...turn,
					messages: turn.messages.map((candidate) => {
						const match =
							"id" in candidate &&
							"id" in message &&
							candidate.id === message.id;
						if (match) replaced = true;
						return match ? message : candidate;
					}),
				}
			: turn,
	);
	return replaced ? next : appendOrCreateTurnMessage(prev, turnId, message);
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
	const [turns, setTurns] = createSignal(props.runtime.getTurns());
	const [pendingAssistantText, setPendingAssistantText] = createSignal("");

	const unsubscribeTurnStarted = props.runtime.subscribe(
		"agent.turn.started",
		(event) => {
			setTurns((prev) => [...prev, { id: event.turn.id, messages: [] }]);
		},
	);

	const unsubscribeUserMessageCreated = props.runtime.subscribe(
		"user.message.created",
		(event) => {
			setTurns((prev) => appendTurnMessage(prev, event.turn.id, event.message));
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
			setTurns((prev) =>
				appendTurnMessage(
					prev,
					event.turn.id,
					finalizeBufferedAssistantMessage(
						pendingAssistantText(),
						event.message,
					),
				),
			);
			setPendingAssistantText("");
		},
	);

	const unsubscribeBashCommandStarted = props.runtime.subscribe(
		"bash.command.started",
		(event) => {
			setTurns((prev) =>
				appendOrCreateTurnMessage(prev, event.turn.id, event.message),
			);
		},
	);

	const unsubscribeBashCommandCompleted = props.runtime.subscribe(
		"bash.command.completed",
		(event) => {
			setTurns((prev) =>
				replaceTurnMessage(prev, event.turn.id, event.message),
			);
		},
	);

	const unsubscribeSessionChanged = props.runtime.subscribe(
		"session.active.changed",
		(_) => {
			setPendingAssistantText("");
			setTurns(props.runtime.getTurns());
		},
	);

	const unsubscribeCompacted = props.runtime.subscribe(
		{ prefix: "session.compaction.completed" },
		(_) => {
			setTurns(props.runtime.getTurns());
		},
	);

	onCleanup(() => {
		unsubscribeTurnStarted();
		unsubscribeUserMessageCreated();
		unsubscribeAgentMessageStarted();
		unsubscribeAgentMessageUpdated();
		unsubscribeAgentMessageEnded();
		unsubscribeBashCommandStarted();
		unsubscribeBashCommandCompleted();
		unsubscribeSessionChanged();
		unsubscribeCompacted();
	});

	return <TranscriptPane {...props} turns={turns()} />;
}
