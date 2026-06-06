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
 * Collapsible tool drawer chip.
 * Collapsed: ▸ N tool calls  Read · Grep · Edit  (on bgSurface)
 * Expanded: ▾ N tool calls  with full tool details indented below
 */
export function CompletedToolSummary(props: {
	toolCalls: ToolCall[];
	toolResults: Map<string, ToolResultMessage>;
	aborted?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(false);
	const renderer = useRenderer();

	const MAX_VISIBLE_TOOLS = 8;

	const count = () => props.toolCalls.length;
	const countLabel = () => `${count()} tool call${count() === 1 ? "" : "s"}`;

	// Safe to read non-reactively: this component only mounts when all
	// tool calls are completed, so all results are present at mount time.
	const toolNameSummary = () => {
		const visible = props.toolCalls.slice(0, MAX_VISIBLE_TOOLS);
		const overflow = props.toolCalls.length - MAX_VISIBLE_TOOLS;
		const names = visible.map((tc) => tc.name);
		const joined = names.join(` ${MIDDLE_DOT} `);
		return overflow > 0 ? `${joined} ${MIDDLE_DOT} +${overflow} more` : joined;
	};

	return (
		<box
			flexDirection="column"
			gap={0}
			backgroundColor={theme.bgSurface}
			paddingX={1}
			width="100%"
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
					<text fg={theme.textPlaceholder}>{toolNameSummary()}</text>
				</Show>
			</box>
			<Show when={expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0}>
					<For each={props.toolCalls}>
						{(tc) => {
							const result = () => props.toolResults.get(tc.id);
							return (
								<Show when={result()}>
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

/**
 * In-progress tool calls: show live/pending rows for tools that are still running,
 * plus a compact summary for any already completed.
 */
export function InProgressToolCalls(props: {
	toolCalls: ToolCall[];
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
}) {
	return (
		<box flexDirection="column" gap={0} width="100%">
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
									fallback={<PendingToolCall tc={tc} aborted={props.aborted} />}
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
	);
}

export function AssistantEntry(props: {
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

	// All tool calls are completed when every tool call has a result
	const allCompleted = () =>
		hasToolCalls && toolCalls.every((tc) => props.toolResults.has(tc.id));

	return (
		<box
			flexDirection="column"
			gap={!props.zenMode && hasToolCalls && hasText ? 1 : 0}
			width="100%"
		>
			<Show when={!props.zenMode && hasToolCalls}>
				<Show
					when={allCompleted()}
					fallback={
						<InProgressToolCalls
							toolCalls={toolCalls}
							toolResults={props.toolResults}
							liveTools={props.liveTools}
							aborted={props.aborted}
						/>
					}
				>
					<CompletedToolSummary
						toolCalls={toolCalls}
						toolResults={props.toolResults}
						aborted={props.aborted}
					/>
				</Show>
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
