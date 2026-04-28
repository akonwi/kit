import { createSignal, ErrorBoundary, onCleanup } from "solid-js";
import type { KitAgentMessage, Turn } from "../session/types";
import { TranscriptPane, type TranscriptPaneProps } from "./TranscriptPane";

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
		unsubscribeSessionChanged();
	});

	return (
		<ErrorBoundary fallback={<TranscriptPane {...props} turns={props.turns} />}>
			<TranscriptPane {...props} turns={turns()} />
		</ErrorBoundary>
	);
}
