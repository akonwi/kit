type Disposer = () => void;

type TurnStatusEventMap = {
	"agent.turn.started": { turn: { id: string } };
	"agent.turn.completed": { turn: { id: string } | null };
	"agent.retry.failed": unknown;
	"agent.run.failed": unknown;
};

type TurnStatusRuntime = {
	subscribe<K extends keyof TurnStatusEventMap>(
		type: K,
		handler: (event: TurnStatusEventMap[K]) => void,
	): Disposer;
	getStatus(): { isStreaming: boolean };
};

/** Keep terminal-level turn status independent from reloadable plugins. */
export function registerTerminalTurnStatus(
	runtime: TurnStatusRuntime,
	setActive: (active: boolean) => void,
): Disposer {
	let activeTurnId: string | null = null;
	const unsubscribeStarted = runtime.subscribe(
		"agent.turn.started",
		(event) => {
			activeTurnId = event.turn.id;
			setActive(true);
		},
	);
	const unsubscribeCompleted = runtime.subscribe(
		"agent.turn.completed",
		(event) => {
			// Agent state can still report streaming while finalization publishes
			// completion. Match the completed turn directly, while ignoring a delayed
			// completion from an older turn.
			if (!event.turn) {
				if (runtime.getStatus().isStreaming) return;
			} else if (event.turn.id !== activeTurnId) {
				return;
			}
			activeTurnId = null;
			setActive(false);
		},
	);
	const clearAfterFailure = () => {
		// Failure events do not carry a turn identity. Preserve a newer turn when
		// one is already streaming; otherwise clear the terminal status.
		if (runtime.getStatus().isStreaming) return;
		activeTurnId = null;
		setActive(false);
	};
	const unsubscribeRetryFailed = runtime.subscribe(
		"agent.retry.failed",
		clearAfterFailure,
	);
	const unsubscribeRunFailed = runtime.subscribe(
		"agent.run.failed",
		clearAfterFailure,
	);

	return () => {
		unsubscribeStarted();
		unsubscribeCompleted();
		unsubscribeRetryFailed();
		unsubscribeRunFailed();
		setActive(false);
	};
}
