import { TextAttributes } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { createSignal, For, Show } from "solid-js";
import type {
	AssistantMessage,
	ToolCall,
	ToolResultMessage,
} from "../../runtime/agent";
import {
	CHECK,
	CIRCLE_SLASH,
	CROSS,
	TRIANGLE_DOWN,
	TRIANGLE_RIGHT,
} from "../glyphs";
import { syntaxStyle, theme } from "../theme";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import { extractToolProgressLines } from "../transcript-live-tools";
import { DrawerChip } from "./drawer-chip";
import { InlineSpinner } from "./inline-spinner";
import {
	extractAssistantParts,
	extractToolResultLines,
	formatToolArgs,
	isAssistantError,
} from "./turns";

const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;

function toolAccentColor(toolName: string): string {
	return toolName === "subagent" ? theme.subagentText : theme.toolText;
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
 * Collapsible drawer for tool calls in a single assistant message.
 *
 * Header: `▸ N tool calls  Read · Grep · Edit` (or spinner when running).
 * Expanded body: per-tool detail rows via Pending/Live/Completed.
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
	return (
		<DrawerChip
			drawerId={props.drawerId}
			toolCalls={props.toolCalls}
			toolResults={props.toolResults}
			aborted={props.aborted}
		>
			<box paddingLeft={2} flexDirection="column" gap={0}>
				<For each={props.toolCalls}>
					{(tc) => (
						<PerToolRow
							tc={tc}
							toolResults={props.toolResults}
							liveTools={props.liveTools}
							aborted={props.aborted}
						/>
					)}
				</For>
			</box>
		</DrawerChip>
	);
}

/**
 * Per-tool row dispatcher. Picks Pending, Live, or Completed presentation
 * based on whether a result has landed and whether live progress is
 * available.
 */
function PerToolRow(props: {
	tc: ToolCall;
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
}) {
	const result = () => props.toolResults.get(props.tc.id);
	const liveTool = () => props.liveTools[props.tc.id];
	return (
		<Show
			when={result()}
			fallback={
				<Show
					when={liveTool()}
					fallback={<PendingToolCall tc={props.tc} aborted={props.aborted} />}
				>
					{(live) => (
						<LiveToolCall
							tc={props.tc}
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
				<CompletedToolCall tc={props.tc} result={r()} aborted={props.aborted} />
			)}
		</Show>
	);
}

/**
 * Flat assistant rendering for use inside a turn-work drawer's expanded
 * view. Renders prose + per-tool rows directly, without wrapping tool
 * calls in another drawer.
 */
export function FlatAssistantEntry(props: {
	msg: AssistantMessage;
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
}) {
	if (isAssistantError(props.msg)) {
		return <text fg={theme.errorText}>{props.msg.errorMessage}</text>;
	}

	const { text, toolCalls } = extractAssistantParts(props.msg);
	const hasText = text.length > 0;
	const hasTools = toolCalls.length > 0;

	return (
		<box flexDirection="column" gap={hasText && hasTools ? 1 : 0} width="100%">
			<Show when={hasText}>
				<markdown
					content={text}
					syntaxStyle={syntaxStyle()}
					conceal
					fg={props.aborted ? theme.textMuted : theme.textPrimary}
				/>
			</Show>
			<Show when={hasTools}>
				<box flexDirection="column" gap={0}>
					<For each={toolCalls}>
						{(tc) => (
							<PerToolRow
								tc={tc}
								toolResults={props.toolResults}
								liveTools={props.liveTools}
								aborted={props.aborted}
							/>
						)}
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
			gap={hasToolCalls && hasText ? 1 : 0}
			width="100%"
		>
			<Show when={hasToolCalls}>
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
