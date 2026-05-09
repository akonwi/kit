import { useRenderer } from "@opentui/solid";
import { createMemo, createSignal, For, Show } from "solid-js";
import { formatTimeAgo } from "../../features/commands/utils";
import { HORIZONTAL_LINE, TRIANGLE_DOWN, TRIANGLE_RIGHT } from "../glyphs";
import { theme } from "../theme";
import { extractAssistantParts, type HandoffSummaryMessage } from "./turns";

const HORIZONTAL_PADDING = 1;

export function HandoffSummaryEntry(props: {
	msg: HandoffSummaryMessage;
	aborted?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(false);
	const [rowWidth, setRowWidth] = createSignal(0);
	const renderer = useRenderer();
	let rowRef: { width: number; height: number } | undefined;
	const { text } = extractAssistantParts(props.msg);
	const lines = () => text.split("\n");
	const sourceLabel = () => props.msg.synthetic?.sourceSessionName?.trim();
	const timestampLabel = () => formatTimeAgo(new Date(props.msg.timestamp));
	const centerLabel = createMemo(() => {
		const parts = [
			`${expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT} merged handoff summary`,
		];
		const source = sourceLabel();
		if (source) parts.push(source);
		parts.push(timestampLabel());
		return parts.join(" · ");
	});
	const divider = createMemo(() => {
		const contentWidth = Math.max(0, rowWidth() - HORIZONTAL_PADDING * 2);
		const sideWidth = Math.max(
			0,
			Math.floor((contentWidth - centerLabel().length - 2) / 2),
		);
		return HORIZONTAL_LINE.repeat(sideWidth);
	});

	return (
		<box flexDirection="column" gap={1} width="100%">
			<box
				ref={(value) => {
					rowRef = value as typeof rowRef;
					if (rowRef) setRowWidth(rowRef.width);
				}}
				onSizeChange={() => {
					if (rowRef) setRowWidth(rowRef.width);
				}}
				flexDirection="row"
				gap={1}
				alignItems="center"
				justifyContent="center"
				width="100%"
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					setExpanded(!expanded());
				}}
			>
				<text fg={theme.borderDefault}>{divider()}</text>
				<box
					flexDirection="row"
					justifyContent="center"
					gap={0}
					paddingX={HORIZONTAL_PADDING}
				>
					<text fg={theme.textSecondary}>
						{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT} merged handoff summary
					</text>
					<Show when={sourceLabel()}>
						{(source) => <text fg={theme.textMuted}> · {source()}</text>}
					</Show>
					<text fg={theme.textMuted}> · {timestampLabel()}</text>
				</box>
				<text fg={theme.borderDefault}>{divider()}</text>
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
