import { useRenderer } from "@opentui/solid";
import {
	type Accessor,
	createMemo,
	createSignal,
	For,
	type ParentProps,
	type Setter,
	Show,
} from "solid-js";
import type { ToolCall, ToolResultMessage } from "../../runtime/agent";
import { MIDDLE_DOT, TRIANGLE_DOWN, TRIANGLE_RIGHT } from "../glyphs";
import { theme } from "../theme";
import { InlineSpinner } from "./inline-spinner";

const MAX_VISIBLE_TOOLS = 8;

/**
 * Module-level store for drawer expansion state, keyed by a stable drawerId
 * derived from the underlying transcript item id. This preserves user-toggled
 * expansion across remounts triggered by new items arriving in the same
 * drawer.
 */
const drawerExpansionSignals = new Map<
	string,
	[Accessor<boolean>, Setter<boolean>]
>();

export function useDrawerExpansion(
	id: string,
): [Accessor<boolean>, Setter<boolean>] {
	let entry = drawerExpansionSignals.get(id);
	if (!entry) {
		entry = createSignal(false);
		drawerExpansionSignals.set(id, entry);
	}
	return entry;
}

function nameColor(toolName: string): string {
	return toolName === "subagent" ? theme.subagentText : theme.textPlaceholder;
}

/**
 * Shared chip wrapper used by ToolDrawer (single-message tool calls) and
 * TurnWorkDrawer (consolidated intermediate turn work).
 *
 * Renders the bgSurface chip with the standard header:
 *   - Chevron (or spinner while any tool is still running)
 *   - "N tool call(s)" count
 *   - Inline preview of tool names when collapsed, subagent calls in purple
 *
 * The expanded body is supplied as children; the chip only mounts children
 * when expanded.
 */
export function DrawerChip(
	props: ParentProps<{
		/** Stable identifier used to persist expansion state across remounts. */
		drawerId: string;
		toolCalls: ToolCall[];
		toolResults: Map<string, ToolResultMessage>;
		aborted?: boolean;
	}>,
) {
	const [expanded, setExpanded] = useDrawerExpansion(props.drawerId);
	const renderer = useRenderer();

	const countLabel = createMemo(() => {
		const n = props.toolCalls.length;
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
			flexDirection="column"
			gap={0}
			backgroundColor={theme.bgSurface}
			paddingX={1}
		>
			<box
				flexDirection="row"
				gap={1}
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					setExpanded(!expanded());
				}}
			>
				<Show
					when={inProgress()}
					fallback={
						<text fg={theme.textMuted}>
							{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}
						</text>
					}
				>
					<InlineSpinner />
				</Show>
				<text fg={theme.textMuted}>{countLabel()}</text>
				<Show when={!expanded() && props.toolCalls.length > 0}>
					<box flexDirection="row" gap={0}>
						<For each={visibleToolCalls()}>
							{(tc, i) => (
								<>
									<Show when={i() > 0}>
										<text fg={theme.textPlaceholder}>{` ${MIDDLE_DOT} `}</text>
									</Show>
									<text fg={nameColor(tc.name)}>{tc.name}</text>
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
			<Show when={expanded()}>{props.children}</Show>
		</box>
	);
}
