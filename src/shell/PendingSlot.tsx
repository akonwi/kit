import { createSignal, onCleanup, Show } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { theme } from "./theme";

const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
const SPINNER_INTERVAL = 80;

export type PanelHostProps = {
	runtime: AgentRuntime;
	pendingMessages: string[];
};

function Spinner() {
	const [frame, setFrame] = createSignal(0);
	const timer = setInterval(() => {
		setFrame((f) => (f + 1) % SPINNER_FRAMES.length);
	}, SPINNER_INTERVAL);
	onCleanup(() => clearInterval(timer));

	return <text fg={theme.panelText}>{SPINNER_FRAMES[frame()]}</text>;
}

function normalizeThinkingText(text: string): string {
	return text.replace(/\s+/g, " ").trim();
}

function showPending(setMessage: (value: string) => void, text: string): void {
	setMessage(text);
}

function clearPending(setMessage: (value: string) => void): void {
	setMessage("");
}

export function PendingSlot(props: PanelHostProps) {
	const [message, setMessage] = createSignal("");

	const unsubscribeTurnStarted = props.runtime.subscribe(
		"agent.turn.started",
		() => {
			showPending(setMessage, "Working…");
		},
	);
	const unsubscribeStarted = props.runtime.subscribe(
		"agent.thinking.started",
		() => {
			showPending(setMessage, "Thinking…");
		},
	);
	const unsubscribeUpdated = props.runtime.subscribe(
		"agent.thinking.updated",
		(event) => {
			setMessage((current) => {
				const prefix = current === "Thinking…" ? "" : current;
				const next = normalizeThinkingText(`${prefix}${event.delta}`);
				return next.length > 0 ? next : "Thinking…";
			});
		},
	);
	const unsubscribeThinkingCompleted = props.runtime.subscribe(
		"agent.thinking.completed",
		() => {
			showPending(setMessage, "Working…");
		},
	);
	const unsubscribeTurnCompleted = props.runtime.subscribe(
		"agent.turn.completed",
		() => {
			clearPending(setMessage);
		},
	);
	const unsubscribeMergeStarted = props.runtime.subscribe(
		"session.merge.started",
		() => {
			showPending(setMessage, "Merging child session into parent…");
		},
	);
	const unsubscribeMergeEnded = props.runtime.subscribe(
		"session.merge.ended",
		() => {
			clearPending(setMessage);
		},
	);
	const unsubscribeRetryStarted = props.runtime.subscribe(
		"agent.retry.started",
		(event) => {
			showPending(
				setMessage,
				`Retrying (${event.attempt}/${event.maxAttempts}) in ${Math.ceil(event.delayMs / 1000)}s…`,
			);
		},
	);
	const unsubscribeRetryFailed = props.runtime.subscribe(
		"agent.retry.failed",
		() => {
			clearPending(setMessage);
		},
	);
	const unsubscribeAutoCompactionStarted = props.runtime.subscribe(
		"session.compaction.started.auto",
		(event) => {
			showPending(setMessage, `Compacting session… (${event.contextPercent}%)`);
		},
	);
	const unsubscribeAutoCompactionCompleted = props.runtime.subscribe(
		"session.compaction.completed.auto",
		() => {
			clearPending(setMessage);
		},
	);
	const unsubscribeAutoCompactionFailed = props.runtime.subscribe(
		"session.compaction.failed.auto",
		() => {
			clearPending(setMessage);
		},
	);
	const unsubscribeRecoveryCompactionStarted = props.runtime.subscribe(
		"session.compaction.started.recovery",
		() => {
			showPending(setMessage, "Compacting session for retry…");
		},
	);
	const unsubscribeRecoveryCompactionCompleted = props.runtime.subscribe(
		"session.compaction.completed.recovery",
		() => {
			clearPending(setMessage);
		},
	);
	const unsubscribeRecoveryCompactionFailed = props.runtime.subscribe(
		"session.compaction.failed.recovery",
		() => {
			clearPending(setMessage);
		},
	);
	const unsubscribeAdaptationCompactionStarted = props.runtime.subscribe(
		"session.compaction.started.adaptation",
		(event) => {
			showPending(
				setMessage,
				`Adapting session to ${event.modelName ?? event.modelId}… (${event.contextPercent}%)`,
			);
		},
	);
	const unsubscribeAdaptationCompactionCompleted = props.runtime.subscribe(
		"session.compaction.completed.adaptation",
		() => {
			clearPending(setMessage);
		},
	);
	const unsubscribeAdaptationCompactionFailed = props.runtime.subscribe(
		"session.compaction.failed.adaptation",
		() => {
			clearPending(setMessage);
		},
	);

	onCleanup(() => {
		unsubscribeTurnStarted();
		unsubscribeStarted();
		unsubscribeUpdated();
		unsubscribeThinkingCompleted();
		unsubscribeTurnCompleted();
		unsubscribeMergeStarted();
		unsubscribeMergeEnded();
		unsubscribeRetryStarted();
		unsubscribeRetryFailed();
		unsubscribeAutoCompactionStarted();
		unsubscribeAutoCompactionCompleted();
		unsubscribeAutoCompactionFailed();
		unsubscribeRecoveryCompactionStarted();
		unsubscribeRecoveryCompactionCompleted();
		unsubscribeRecoveryCompactionFailed();
		unsubscribeAdaptationCompactionStarted();
		unsubscribeAdaptationCompactionCompleted();
		unsubscribeAdaptationCompactionFailed();
	});

	return (
		<box flexShrink={0} flexDirection="column" gap={0}>
			<Show when={props.pendingMessages.length > 0}>
				<box flexDirection="column" paddingLeft={1} paddingRight={1}>
					{props.pendingMessages.map((message, index) => (
						<box paddingLeft={1} paddingRight={1}>
							<text fg={theme.textMuted}>
								{`Follow-up ${index + 1}: ${message.replace(/\s+/g, " ").trim()}`}
							</text>
						</box>
					))}
				</box>
			</Show>

			<box
				flexShrink={0}
				height={1}
				paddingLeft={1}
				paddingRight={1}
				flexDirection="row"
				gap={2}
			>
				<Show when={message().length > 0}>
					<Spinner />
					<text fg={theme.panelText}>{message()}</text>
				</Show>
			</box>
		</box>
	);
}
