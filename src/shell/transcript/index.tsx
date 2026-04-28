import { createSignal, onCleanup } from "solid-js";
import type { KitAgentMessage, Turn } from "../../session/types";
import { TranscriptPane, type TranscriptPaneProps } from "./pane";

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

export type { TranscriptPaneProps } from "./pane";

export function Transcript(props: TranscriptPaneProps) {
	const [turns, setTurns] = createSignal(props.turns);
	let lastSessionId = props.runtime.getSession().id;

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

	const unsubscribeAgentMessageEnded = props.runtime.subscribe(
		"agent.message.ended",
		(event) => {
			setTurns((prev) => appendTurnMessage(prev, event.turn.id, event.message));
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
		"session.changed",
		(event) => {
			if (event.session.id === lastSessionId) return;
			lastSessionId = event.session.id;
			setTurns(props.runtime.getTurns());
		},
	);

	onCleanup(() => {
		unsubscribeTurnStarted();
		unsubscribeUserMessageCreated();
		unsubscribeAgentMessageEnded();
		unsubscribeBashCommandStarted();
		unsubscribeBashCommandCompleted();
		unsubscribeSessionChanged();
	});

	return <TranscriptPane {...props} turns={turns()} />;
}
