import type { BorderSides } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { createSignal, For, Show } from "solid-js";
import {
	CHECK,
	CIRCLE_SLASH,
	CROSS,
	TRIANGLE_DOWN,
	TRIANGLE_RIGHT,
} from "../glyphs";
import { syntaxStyle, theme } from "../theme";
import { InlineSpinner } from "./inline-spinner";
import type { BashExecutionMessage } from "./turns";

export function BashEntry(props: {
	msg: BashExecutionMessage;
	noTruncate?: boolean;
	/**
	 * Renders without the left-border accent frame and collapsed by
	 * default. Used in the turn activity view where entries read as flat
	 * log lines.
	 */
	plain?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(!props.plain);
	const renderer = useRenderer();
	const output = () => props.msg.output ?? "";
	const outputLines = () => (output().length > 0 ? output().split("\n") : []);
	const hasOutput = () => outputLines().length > 0;
	/** The command is still running when exitCode hasn't been set yet. */
	const pending = () =>
		props.msg.exitCode === undefined && !props.msg.cancelled;
	const prefix = () =>
		pending()
			? null
			: props.msg.cancelled
				? CIRCLE_SLASH
				: props.msg.exitCode === 0
					? CHECK
					: CROSS;
	const prefixColor = () =>
		pending()
			? theme.toolText
			: props.msg.cancelled
				? theme.textMuted
				: props.msg.exitCode === 0
					? theme.toolText
					: theme.errorText;

	const displayLines = () => {
		if (!expanded()) return [];
		if (!props.noTruncate && outputLines().length > 20) {
			return [
				...outputLines().slice(0, 18),
				`  ... (${outputLines().length - 18} more lines)`,
			];
		}
		return outputLines();
	};

	return (
		<box
			border={props.plain ? false : ["left" as BorderSides]}
			borderColor={theme.toolText}
			paddingLeft={props.plain ? 0 : 1}
			flexDirection="column"
			gap={0}
			width="100%"
		>
			<box
				flexDirection="row"
				gap={1}
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					if (hasOutput()) setExpanded(!expanded());
				}}
			>
				<Show
					when={pending()}
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
				<Show when={!pending() && hasOutput()}>
					<text fg={theme.metaText}>
						{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}
					</text>
				</Show>
			</box>
			<Show when={!pending() && expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0}>
					<For each={displayLines()}>
						{(line) => <text fg={theme.textMuted}>{line}</text>}
					</For>
				</box>
			</Show>
		</box>
	);
}
