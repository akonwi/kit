import { useRenderer } from "@opentui/solid";
import { createMemo, For, Show } from "solid-js";
import type { ToolCall, ToolResultMessage } from "../../runtime/agent";
import { MIDDLE_DOT, TRIANGLE_RIGHT } from "../glyphs";
import { theme } from "../theme";
import { InlineSpinner } from "./inline-spinner";
import { toolDisplayName } from "./turns";

const MAX_VISIBLE_TOOLS = 8;

function nameColor(toolName: string): string {
	return toolName === "subagent" ? theme.subagentText : theme.textPlaceholder;
}

/**
 * Compact chip used as the visible affordance for a tool drawer (single
 * assistant message) or a turn-work drawer (consolidated intermediate work).
 *
 * Clicking the chip invokes `onActivate` — currently used by callers to open
 * the activity dialog. The chip itself does not manage any expanded state.
 *
 *   ▸ N tool calls  Read · Grep · Edit       (idle)
 *   ⠋ N tool calls  Read · Grep · Edit       (any tool still running)
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

	return (
		<box
			flexDirection="row"
			gap={1}
			backgroundColor={theme.bgSurface}
			paddingX={1}
			paddingY={1}
			onMouseDown={() => {
				if (renderer.getSelection()?.getSelectedText()) return;
				props.onActivate?.();
			}}
		>
			<Show
				when={inProgress()}
				fallback={<text fg={theme.textMuted}>{TRIANGLE_RIGHT}</text>}
			>
				<InlineSpinner />
			</Show>
			<text fg={theme.textMuted}>{countLabel()}</text>
			<Show when={props.toolCalls.length > 0}>
				<box flexDirection="row" gap={0}>
					<For each={visibleToolCalls()}>
						{(tc, i) => (
							<>
								<Show when={i() > 0}>
									<text fg={theme.textPlaceholder}>{` ${MIDDLE_DOT} `}</text>
								</Show>
								<text fg={nameColor(tc.name)}>{toolDisplayName(tc)}</text>
							</>
						)}
					</For>
					<Show when={overflowCount() > 0}>
						<text fg={theme.textPlaceholder}>
							{` ${MIDDLE_DOT} +${overflowCount()} more`}
						</text>
					</Show>
				</box>
			</Show>
		</box>
	);
}
