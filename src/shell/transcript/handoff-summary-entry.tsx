import { useRenderer } from "@opentui/solid";
import { createSignal, For, Show } from "solid-js";
import { formatTimeAgo } from "../../features/commands/utils";
import { theme } from "../theme";
import { extractAssistantParts, type HandoffSummaryMessage } from "./turns";

export function HandoffSummaryEntry(props: {
	msg: HandoffSummaryMessage;
	aborted?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(false);
	const renderer = useRenderer();
	const { text } = extractAssistantParts(props.msg);
	const lines = () => text.split("\n");
	const sourceLabel = () => props.msg.synthetic?.sourceSessionName?.trim();
	const timestampLabel = () => formatTimeAgo(new Date(props.msg.timestamp));

	return (
		<box flexDirection="column" gap={1} width="100%">
			<box
				flexDirection="row"
				gap={0}
				width="100%"
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					setExpanded(!expanded());
				}}
			>
				<text fg={theme.textMuted}>────── </text>
				<text fg={theme.textSecondary}>
					{expanded() ? "▾" : "▸"} merged handoff summary
				</text>
				<Show when={sourceLabel()}>
					{(source) => <text fg={theme.textMuted}> · {source()}</text>}
				</Show>
				<text fg={theme.textMuted}> · {timestampLabel()}</text>
			</box>
			<Show when={expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0} width="100%">
					<For each={lines()}>
						{(line) => {
							const trimmed = line.trim();
							if (trimmed.length === 0) {
								return <text fg={theme.textSecondary}>{""}</text>;
							}
							if (trimmed.startsWith("## ")) {
								return <text fg={theme.textPrimary}>{trimmed.slice(3)}</text>;
							}
							return <text fg={theme.textSecondary}>{line}</text>;
						}}
					</For>
				</box>
			</Show>
		</box>
	);
}
