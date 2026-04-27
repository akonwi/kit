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

export function PendingSlot(props: PanelHostProps) {
	const [isPending, setIsPending] = createSignal(false);
	const [thinkingText, setThinkingText] = createSignal("");

	const unsubscribeTurnStarted = props.runtime.subscribe(
		"agent.turn.started",
		() => {
			setIsPending(true);
			setThinkingText("Working…");
		},
	);
	const unsubscribeStarted = props.runtime.subscribe(
		"agent.thinking.started",
		() => {
			setIsPending(true);
			setThinkingText("Thinking…");
		},
	);
	const unsubscribeUpdated = props.runtime.subscribe(
		"agent.thinking.updated",
		(event) => {
			setThinkingText((current) => {
				const prefix = current === "Thinking…" ? "" : current;
				const next = normalizeThinkingText(`${prefix}${event.delta}`);
				return next.length > 0 ? next : "Thinking…";
			});
		},
	);
	const unsubscribeThinkingCompleted = props.runtime.subscribe(
		"agent.thinking.completed",
		() => {
			setIsPending(true);
			setThinkingText("Working…");
		},
	);
	const unsubscribeTurnCompleted = props.runtime.subscribe(
		"agent.turn.completed",
		() => {
			setIsPending(false);
			setThinkingText("");
		},
	);

	onCleanup(() => {
		unsubscribeTurnStarted();
		unsubscribeStarted();
		unsubscribeUpdated();
		unsubscribeThinkingCompleted();
		unsubscribeTurnCompleted();
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
				<Show when={isPending()}>
					<Spinner />
					<text fg={theme.panelText}>{thinkingText()}</text>
				</Show>
			</box>
		</box>
	);
}
