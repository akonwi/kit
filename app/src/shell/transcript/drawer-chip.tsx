import { useRenderer } from "@opentui/solid";
import { createMemo, Show } from "solid-js";
import type { ToolCall, ToolResultMessage } from "../../runtime/agent";
import { CHEVRON_RIGHT, MIDDLE_DOT } from "../glyphs";
import { theme } from "../theme";
import { InlineSpinner } from "./inline-spinner";
import { toolDisplayName } from "./turns";

const MAX_VISIBLE_TOOLS = 8;

/**
 * Compact chip used as the visible affordance for a tool drawer (single
 * assistant message) or a turn-work drawer (consolidated intermediate work).
 *
 * Clicking the chip invokes `onActivate` — currently used by callers to open
 * the activity panel. The chip itself does not manage any expanded state.
 *
 *   › N tool calls  Read · Grep · Edit       (idle)
 *   ⠋ N tool calls  Read · Grep · Edit       (any tool still running)
 *
 * `›` is used rather than `▸` because clicking opens a separate panel/
 * dialog rather than expanding inline — see CHEVRON_RIGHT in the design
 * SKILL.md glyph catalog.
 */
export function DrawerChip(props: {
	toolCalls: ToolCall[];
	toolResults: Map<string, ToolResultMessage>;
	aborted?: boolean;
	onActivate?: () => void;
	/**
	 * Label shown in place of the tool-call count when `toolCalls` is empty.
	 * Lets a turn-work chip summarize non-tool activity (bash, handoffs)
	 * without showing the misleading "0 tool calls".
	 */
	emptyLabel?: string;
}) {
	const renderer = useRenderer();

	const countLabel = createMemo(() => {
		const n = props.toolCalls.length;
		if (n === 0 && props.emptyLabel) return props.emptyLabel;
		return `${n} tool call${n === 1 ? "" : "s"}`;
	});

	const inProgress = createMemo(
		() =>
			!props.aborted &&
			props.toolCalls.some((tc) => !props.toolResults.has(tc.id)),
	);

	const visibleToolCalls = createMemo(() =>
		props.toolCalls.slice(0, MAX_VISIBLE_TOOLS),
	);
	const overflowCount = createMemo(() =>
		Math.max(0, props.toolCalls.length - MAX_VISIBLE_TOOLS),
	);
	const toolSummary = createMemo(() => {
		const names = visibleToolCalls()
			.map(toolDisplayName)
			.join(` ${MIDDLE_DOT} `);
		return overflowCount() > 0
			? `${names} ${MIDDLE_DOT} +${overflowCount()} more`
			: names;
	});

	return (
		<box
			flexDirection="row"
			gap={1}
			width="100%"
			height={1}
			overflow="hidden"
			backgroundColor={theme.bgSurface}
			paddingX={1}
			onMouseDown={(event) => {
				if (renderer.getSelection()?.getSelectedText()) return;
				event.preventDefault();
				event.stopPropagation();
				props.onActivate?.();
			}}
			onMouseUp={(event) => {
				event.preventDefault();
				event.stopPropagation();
			}}
		>
			<Show
				when={inProgress()}
				fallback={<text fg={theme.textMuted}>{CHEVRON_RIGHT}</text>}
			>
				<InlineSpinner />
			</Show>
			<text fg={theme.textMuted} flexShrink={0} wrapMode="none">
				{countLabel()}
			</text>
			<Show when={props.toolCalls.length > 0}>
				<text
					fg={theme.textPlaceholder}
					flexBasis={0}
					flexGrow={1}
					flexShrink={1}
					wrapMode="none"
					truncate
				>
					{toolSummary()}
				</text>
			</Show>
		</box>
	);
}
