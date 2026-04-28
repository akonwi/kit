import type { BorderSides } from "@opentui/core";
import { createSignal, For, Show } from "solid-js";
import { syntaxStyle, theme } from "../theme";
import { InlineSpinner } from "./inline-spinner";
import type { BashExecutionMessage } from "./turns";

export function BashEntry(props: { msg: BashExecutionMessage }) {
	const [expanded, setExpanded] = createSignal(true);
	const outputLines = () =>
		props.msg.output.length > 0 ? props.msg.output.split("\n") : [];
	const hasOutput = () => outputLines().length > 0;
	const prefix = () =>
		props.msg.pending
			? null
			: props.msg.cancelled
				? "⊘"
				: props.msg.exitCode === 0
					? "✓"
					: "✗";
	const prefixColor = () =>
		props.msg.pending
			? theme.toolText
			: props.msg.cancelled
				? theme.textMuted
				: props.msg.exitCode === 0
					? theme.toolText
					: theme.errorText;

	const displayLines = () => {
		if (!expanded()) return [];
		if (outputLines().length > 20) {
			return [
				...outputLines().slice(0, 18),
				`  ... (${outputLines().length - 18} more lines)`,
			];
		}
		return outputLines();
	};

	return (
		<box
			border={["left" as BorderSides]}
			borderColor={theme.toolText}
			paddingLeft={1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<box
				flexDirection="row"
				gap={1}
				onMouseDown={() => hasOutput() && setExpanded(!expanded())}
			>
				<Show
					when={props.msg.pending}
					fallback={<text fg={prefixColor()}>{prefix()}</text>}
				>
					<InlineSpinner />
				</Show>
				<code
					filetype="bash"
					content={props.msg.command}
					syntaxStyle={syntaxStyle()}
					fg={theme.textPrimary}
				/>
				<Show when={props.msg.pending}>
					<text fg={theme.metaText}>running…</text>
				</Show>
				<Show when={!props.msg.pending && hasOutput()}>
					<text fg={theme.metaText}>
						{expanded() ? "▾" : "▸"} {outputLines().length} line
						{outputLines().length === 1 ? "" : "s"}
					</text>
				</Show>
			</box>
			<Show when={!props.msg.pending && expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0}>
					<For each={displayLines()}>
						{(line) => <text fg={theme.textMuted}>{line}</text>}
					</For>
				</box>
			</Show>
		</box>
	);
}
