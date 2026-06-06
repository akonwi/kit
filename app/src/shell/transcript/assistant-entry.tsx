import { TextAttributes } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import {
	type Accessor,
	createMemo,
	createSignal,
	For,
	type Setter,
	Show,
} from "solid-js";
import type {
	AssistantMessage,
	ToolCall,
	ToolResultMessage,
} from "../../runtime/agent";
import {
	CHECK,
	CIRCLE_SLASH,
	CROSS,
	MIDDLE_DOT,
	TRIANGLE_DOWN,
	TRIANGLE_RIGHT,
} from "../glyphs";
import { syntaxStyle, theme } from "../theme";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import { extractToolProgressLines } from "../transcript-live-tools";
import { InlineSpinner } from "./inline-spinner";
import {
	extractAssistantParts,
	extractToolResultLines,
	formatToolArgs,
	isAssistantError,
} from "./turns";

const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;
const MAX_VISIBLE_TOOLS = 8;

function toolAccentColor(toolName: string): string {
	return toolName === "subagent" ? theme.subagentText : theme.toolText;
}

/**
 * Module-level store for ToolDrawer expansion state, keyed by a stable
 * drawerId derived from the underlying transcript item id. This preserves
 * user-toggled expansion across remounts triggered by new tool calls
 * arriving in the same group.
 */
const drawerExpansionSignals = new Map<
	string,
	[Accessor<boolean>, Setter<boolean>]
>();

function useDrawerExpansion(id: string): [Accessor<boolean>, Setter<boolean>] {
	let entry = drawerExpansionSignals.get(id);
	if (!entry) {
		entry = createSignal(false);
		drawerExpansionSignals.set(id, entry);
	}
	return entry;
}

function PendingToolCall(props: { tc: ToolCall; aborted?: boolean }) {
	return (
		<box flexDirection="row" gap={1}>
			<Show
				when={!props.aborted}
				fallback={<text fg={theme.textMuted}>{CIRCLE_SLASH}</text>}
			>
				<InlineSpinner />
			</Show>
			<text
				fg={props.aborted ? theme.textMuted : toolAccentColor(props.tc.name)}
				attributes={props.aborted ? ABORTED_ATTRS : undefined}
			>
				{props.tc.name}
				{formatToolArgs(props.tc.arguments)}
			</text>
		</box>
	);
}

function LiveToolCall(props: {
	tc: ToolCall;
	args?: unknown;
	partialResult?: unknown | null;
	result?: unknown | null;
	isError?: boolean | null;
	state: "started" | "updated" | "ended";
	aborted?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(false);
	const renderer = useRenderer();
	const lines = () =>
		extractToolProgressLines(props.result ?? props.partialResult ?? null);
	const hasOutput = () => lines().length > 0;
	const displayLines = () => {
		if (!expanded()) return [];
		if (lines().length > 40) {
			return [
				...lines().slice(0, 38),
				`  ... (${lines().length - 38} more lines)`,
			];
		}
		return lines();
	};
	const prefix = () => {
		if (props.aborted) return CIRCLE_SLASH;
		if (props.state !== "ended") return null;
		return props.isError ? CROSS : CHECK;
	};
	const accent = () => toolAccentColor(props.tc.name);
	const headerColor = () => {
		if (props.aborted) return theme.textMuted;
		if (props.state !== "ended") return accent();
		return props.isError ? theme.errorText : accent();
	};
	const toolArgs = () =>
		typeof props.args === "object" && props.args !== null
			? (props.args as Record<string, unknown>)
			: props.tc.arguments;

	return (
		<box flexDirection="column" gap={0} width="100%">
			<box
				flexDirection="row"
				gap={1}
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					if (hasOutput()) setExpanded(!expanded());
				}}
			>
				<Show when={prefix()} fallback={<InlineSpinner />}>
					{(value) => <text fg={headerColor()}>{value()}</text>}
				</Show>
				<text
					fg={headerColor()}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
				>
					{props.tc.name}
					{formatToolArgs(toolArgs())}
				</text>
				<Show when={hasOutput() && !props.aborted}>
					<text fg={theme.metaText}>
						{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}
					</text>
				</Show>
			</box>
			<Show when={expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0}>
					<For each={displayLines()}>
						{(line) => <text fg={theme.textMuted}>{line}</text>}
					</For>
				</box>
			</Show>
		</box>
	);
}

function CompletedToolCall(props: {
	tc: ToolCall;
	result: ToolResultMessage;
	aborted?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(false);
	const renderer = useRenderer();
	const lines = extractToolResultLines(props.result);
	const prefix = props.aborted
		? CIRCLE_SLASH
		: props.result.isError
			? CROSS
			: CHECK;
	const accent = toolAccentColor(props.tc.name);
	const headerColor = props.aborted
		? theme.textMuted
		: props.result.isError
			? theme.errorText
			: accent;
	const hasOutput = lines.length > 0;

	const displayLines = () => {
		if (!expanded()) return [];
		if (lines.length > 40) {
			return [...lines.slice(0, 38), `  ... (${lines.length - 38} more lines)`];
		}
		return lines;
	};

	return (
		<box flexDirection="column" gap={0} width="100%">
			<box
				flexDirection="row"
				gap={1}
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					if (hasOutput) setExpanded(!expanded());
				}}
			>
				<text
					fg={headerColor}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
				>
					{prefix} {props.tc.name}
					{formatToolArgs(props.tc.arguments)}
				</text>
				<Show when={hasOutput && !props.aborted}>
					<text fg={theme.metaText}>
						{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}
					</text>
				</Show>
			</box>
			<Show when={expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0}>
					<For each={displayLines()}>
						{(line) => <text fg={theme.textMuted}>{line}</text>}
					</For>
				</box>
			</Show>
		</box>
	);
}

/**
 * Universal collapsible tool drawer.
 *
 * Renders all tool calls (in-progress and completed) in a single bgSurface
 * chip with a count + tool-name summary. A spinner is appended while any
 * call is still running.
 *
 * Collapsed (default): ▸ N tool calls  Read · Grep · Edit  [spinner]
 * Expanded:            ▾ N tool calls  [spinner]
 *                        · per-tool detail rows (Pending/Live/Completed)
 */
export function ToolDrawer(props: {
	/**
	 * Stable identifier (typically the originating transcript item id) used to
	 * persist expansion state across remounts when new tool calls stream into
	 * the same group.
	 */
	drawerId: string;
	toolCalls: ToolCall[];
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
}) {
	const [expanded, setExpanded] = useDrawerExpansion(props.drawerId);
	const renderer = useRenderer();

	const countLabel = createMemo(
		() =>
			`${props.toolCalls.length} tool call${props.toolCalls.length === 1 ? "" : "s"}`,
	);

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

	const nameColor = (toolName: string) =>
		toolName === "subagent" ? theme.subagentText : theme.textPlaceholder;

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
				<text fg={theme.textMuted}>
					{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}
				</text>
				<text fg={theme.textMuted}>{countLabel()}</text>
				<Show when={!expanded()}>
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
				<Show when={inProgress()}>
					<InlineSpinner />
				</Show>
			</box>
			<Show when={expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0}>
					<For each={props.toolCalls}>
						{(tc) => {
							const result = () => props.toolResults.get(tc.id);
							const liveTool = () => props.liveTools[tc.id];
							return (
								<Show
									when={result()}
									fallback={
										<Show
											when={liveTool()}
											fallback={
												<PendingToolCall tc={tc} aborted={props.aborted} />
											}
										>
											{(live) => (
												<LiveToolCall
													tc={tc}
													args={live().args}
													partialResult={live().partialResult}
													result={live().result}
													isError={live().isError}
													state={live().state}
													aborted={props.aborted}
												/>
											)}
										</Show>
									}
								>
									{(r) => (
										<CompletedToolCall
											tc={tc}
											result={r()}
											aborted={props.aborted}
										/>
									)}
								</Show>
							);
						}}
					</For>
				</box>
			</Show>
		</box>
	);
}

export function AssistantEntry(props: {
	/** Stable id of the underlying transcript item, used for drawer state. */
	itemId: string;
	msg: AssistantMessage;
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
	zenMode?: boolean;
}) {
	if (isAssistantError(props.msg)) {
		return (
			<box paddingLeft={1} flexDirection="column" gap={0} width="100%">
				<text fg={theme.errorText}>{props.msg.errorMessage}</text>
			</box>
		);
	}

	const { text, toolCalls } = extractAssistantParts(props.msg);
	const hasToolCalls = toolCalls.length > 0;
	const hasText = text.length > 0;

	return (
		<box
			flexDirection="column"
			gap={!props.zenMode && hasToolCalls && hasText ? 1 : 0}
			width="100%"
		>
			<Show when={!props.zenMode && hasToolCalls}>
				<ToolDrawer
					drawerId={props.itemId}
					toolCalls={toolCalls}
					toolResults={props.toolResults}
					liveTools={props.liveTools}
					aborted={props.aborted}
				/>
			</Show>
			<Show when={hasText}>
				<markdown
					content={text}
					syntaxStyle={syntaxStyle()}
					conceal
					fg={props.aborted ? theme.textMuted : theme.textPrimary}
				/>
			</Show>
		</box>
	);
}
