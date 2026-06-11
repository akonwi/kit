import { createSignal, onCleanup, Show } from "solid-js";
import type { AgentRuntime } from "../runtime/agent-runtime";
import { Spinner } from "./Spinner";
import { syntaxStyle, theme } from "./theme";

export type PanelHostProps = {
	runtime: AgentRuntime;
	pendingMessages: string[];
};

type PendingState = {
	content: string;
};

function normalizeThinkingText(text: string): string {
	return text
		.replace(/\r\n?/g, "\n")
		.split("\n")
		.map((line) => line.trimEnd())
		.join("\n");
}

function showPending(
	setPending: (value: PendingState) => void,
	content: string,
): void {
	setPending({ content });
}

function clearPending(setPending: (value: PendingState) => void): void {
	setPending({ content: "" });
}

export function PendingSlot(props: PanelHostProps) {
	const [pending, setPending] = createSignal<PendingState>({
		content: "",
	});

	const unsubscribeTurnStarted = props.runtime.subscribe(
		"agent.turn.started",
		() => {
			showPending(setPending, "Working…");
		},
	);
	const unsubscribeStarted = props.runtime.subscribe(
		"agent.thinking.started",
		() => {
			showPending(setPending, "Thinking…");
		},
	);
	const unsubscribeUpdated = props.runtime.subscribe(
		"agent.thinking.updated",
		(event) => {
			setPending((current) => {
				const prefix = current.content === "Thinking…" ? "" : current.content;
				const next = normalizeThinkingText(`${prefix}${event.delta}`);
				return {
					content: next.length > 0 ? next : "Thinking…",
				};
			});
		},
	);
	const unsubscribeThinkingCompleted = props.runtime.subscribe(
		"agent.thinking.completed",
		() => {
			showPending(setPending, "Working…");
		},
	);
	const unsubscribeTurnCompleted = props.runtime.subscribe(
		"agent.turn.completed",
		() => {
			clearPending(setPending);
		},
	);
	const unsubscribeMergeStarted = props.runtime.subscribe(
		"session.merge.started",
		() => {
			showPending(setPending, "Merging child session into parent…");
		},
	);
	const unsubscribeMergeEnded = props.runtime.subscribe(
		"session.merge.ended",
		() => {
			clearPending(setPending);
		},
	);
	const unsubscribeRetryStarted = props.runtime.subscribe(
		"agent.retry.started",
		(event) => {
			showPending(
				setPending,
				`Retrying (${event.attempt}/${event.maxAttempts}) in ${Math.ceil(event.delayMs / 1000)}s…`,
			);
		},
	);
	const unsubscribeRetryFailed = props.runtime.subscribe(
		"agent.retry.failed",
		() => {
			clearPending(setPending);
		},
	);
	const unsubscribeAutoCompactionStarted = props.runtime.subscribe(
		"session.compaction.started.auto",
		(event) => {
			showPending(setPending, `Compacting session… (${event.contextPercent}%)`);
		},
	);
	const unsubscribeAutoCompactionCompleted = props.runtime.subscribe(
		"session.compaction.completed.auto",
		() => {
			clearPending(setPending);
		},
	);
	const unsubscribeAutoCompactionFailed = props.runtime.subscribe(
		"session.compaction.failed.auto",
		() => {
			clearPending(setPending);
		},
	);
	const unsubscribeRecoveryCompactionStarted = props.runtime.subscribe(
		"session.compaction.started.recovery",
		() => {
			showPending(setPending, "Compacting session for retry…");
		},
	);
	const unsubscribeRecoveryCompactionCompleted = props.runtime.subscribe(
		"session.compaction.completed.recovery",
		() => {
			clearPending(setPending);
		},
	);
	const unsubscribeRecoveryCompactionFailed = props.runtime.subscribe(
		"session.compaction.failed.recovery",
		() => {
			clearPending(setPending);
		},
	);
	const unsubscribeAdaptationCompactionStarted = props.runtime.subscribe(
		"session.compaction.started.adaptation",
		(event) => {
			showPending(
				setPending,
				`Adapting session to ${event.modelName ?? event.modelId}… (${event.contextPercent}%)`,
			);
		},
	);
	const unsubscribeAdaptationCompactionCompleted = props.runtime.subscribe(
		"session.compaction.completed.adaptation",
		() => {
			clearPending(setPending);
		},
	);
	const unsubscribeAdaptationCompactionFailed = props.runtime.subscribe(
		"session.compaction.failed.adaptation",
		() => {
			clearPending(setPending);
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
			<box
				flexShrink={0}
				height={1}
				paddingLeft={1}
				paddingRight={1}
				flexDirection="row"
				gap={1}
			>
				<Show when={pending().content.length > 0}>
					<Spinner fg={theme.panelText} />
					<box flexGrow={1} height={1} overflow="hidden">
						<markdown
							content={pending().content}
							syntaxStyle={syntaxStyle()}
							conceal
							fg={theme.panelText}
						/>
					</box>
				</Show>
			</box>

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
		</box>
	);
}
