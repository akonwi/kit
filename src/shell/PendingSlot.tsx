import { createSignal, onCleanup, Show } from "solid-js";
import type { PanelState } from "../state/app-state";
import { theme } from "./theme";

const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
const SPINNER_INTERVAL = 80;

export type PanelHostProps = {
	panel: PanelState;
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

export function PendingSlot(props: PanelHostProps) {
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
				gap={1}
			>
				<Show when={props.panel.pending}>
					<Spinner />
					<text fg={theme.panelText}>{props.panel.title}</text>
				</Show>
			</box>
		</box>
	);
}
