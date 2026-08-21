import { TextAttributes } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { diffLines } from "diff";
import { createMemo, createSignal, For, Match, Show, Switch } from "solid-js";
import type {
	AssistantMessage,
	ToolCall,
	ToolResultMessage,
} from "../../runtime/agent";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { CodeView } from "../code-view";
import { ReviewDiffBlock } from "../diff/ReviewDiffBlock";
import type { ReviewHunk, ReviewLine } from "../diff/types";
import { inferFiletype } from "../filetype";
import {
	CHECK,
	CIRCLE_SLASH,
	CROSS,
	ELLIPSIS,
	TRIANGLE_DOWN,
	TRIANGLE_RIGHT,
} from "../glyphs";
import { KitMarkdown } from "../KitMarkdown";
import { syntaxStyle, theme } from "../theme";
import type { LiveToolsForTurn } from "../transcript-live-tools";
import { extractToolProgressLines } from "../transcript-live-tools";
import { DrawerChip } from "./drawer-chip";
import { InlineSpinner } from "./inline-spinner";
import { createMessageContextMenuGesture } from "./message-context-menu";
import {
	extractAssistantMarkdownSource,
	extractAssistantParts,
	extractToolResultLines,
	formatToolArgs,
	isAssistantError,
	presentBashCommand,
	toolArgKeys,
	toolDisplayName,
} from "./turns";
import type { OpenActivity, OpenMessageContextMenu } from "./types";

const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;

function toolAccentColor(toolName: string): string {
	return toolName === "subagent" ? theme.subagentText : theme.toolText;
}

type ToolArgumentPresentation = {
	text: string;
	compact: boolean;
	detail?: string;
};

function toolArgumentPresentation(
	tc: ToolCall,
	args: Record<string, unknown> | undefined,
	fullArgs?: boolean,
): ToolArgumentPresentation {
	const command = args?.command;
	if (tc.name === "bash" && typeof command === "string") {
		const presentation = presentBashCommand(command);
		return {
			text: presentation.text,
			compact: true,
			detail: presentation.summarized ? command : undefined,
		};
	}
	return {
		text: formatToolArgs(args, {
			full: fullArgs,
			keys: toolArgKeys(tc),
		}).trimStart(),
		compact: false,
	};
}

function BashCommandDetail(props: { command?: string }) {
	return (
		<code
			filetype="bash"
			content={props.command ?? ""}
			syntaxStyle={syntaxStyle()}
			fg={theme.textPrimary}
			width="100%"
			visible={props.command !== undefined}
		/>
	);
}

// ── Enriched tool output detection ────────────────────────────────

type EnrichedDetail =
	| { kind: "file"; path: string; content: string }
	| {
			kind: "edits";
			path: string;
			edits: Array<{ oldText: string; newText: string }>;
	  };

function extractResultText(result: ToolResultMessage): string {
	const parts: string[] = [];
	for (const block of result.content) {
		if (block.type === "text" && "text" in block && block.text) {
			parts.push(block.text);
		}
	}
	return parts.join("");
}

function detectEnrichment(
	tc: ToolCall,
	result: ToolResultMessage,
): EnrichedDetail | null {
	if (result.isError) return null;
	const name = tc.name.toLowerCase();
	const args = tc.arguments ?? {};

	if (name === "read" && typeof args.path === "string") {
		const text = extractResultText(result);
		if (!text) return null;
		return { kind: "file", path: args.path, content: text };
	}

	if (
		name === "write" &&
		typeof args.path === "string" &&
		typeof args.content === "string"
	) {
		return { kind: "file", path: args.path, content: args.content };
	}

	if (
		name === "edit" &&
		typeof args.path === "string" &&
		Array.isArray(args.edits)
	) {
		const edits: Array<{ oldText: string; newText: string }> = [];
		for (const e of args.edits) {
			if (
				typeof e === "object" &&
				e !== null &&
				typeof (e as { oldText?: unknown }).oldText === "string" &&
				typeof (e as { newText?: unknown }).newText === "string"
			) {
				edits.push(e as { oldText: string; newText: string });
			}
		}
		if (edits.length === 0) return null;
		return { kind: "edits", path: args.path, edits };
	}

	return null;
}

function FileCodeBlock(props: { path: string; content: string }) {
	// Tool outputs don't carry reliable absolute file positions in every case
	// (e.g. multi-edit, post-hoc reads), so we suppress the gutter rather
	// than risk showing misleading numbers.
	return (
		<CodeView
			path={props.path}
			content={props.content}
			showLineNumbers={false}
		/>
	);
}

function splitDiffPart(value: string): string[] {
	const lines = value.split("\n");
	// diffLines emits a trailing empty entry when the chunk ends with \n.
	if (lines[lines.length - 1] === "") lines.pop();
	return lines;
}

/**
 * Construct a synthetic ReviewHunk from an edit's before/after pair so the
 * dialog can render the diff with the same line-number gutter + bg tints as
 * the code review. Line numbers are 1-based within the hunk since edits
 * don't include absolute file positions.
 */
function buildEditHunk(
	oldText: string,
	newText: string,
	id: string,
): ReviewHunk {
	const parts = diffLines(oldText, newText);
	const lines: ReviewLine[] = [];
	let additionLineNumber = 1;
	let deletionLineNumber = 1;
	let additionCount = 0;
	let deletionCount = 0;

	for (const part of parts) {
		if (part.value.length === 0) continue;
		for (const text of splitDiffPart(part.value)) {
			if (part.added) {
				lines.push({ kind: "add", text, additionLineNumber });
				additionLineNumber++;
				additionCount++;
			} else if (part.removed) {
				lines.push({ kind: "delete", text, deletionLineNumber });
				deletionLineNumber++;
				deletionCount++;
			} else {
				lines.push({
					kind: "context",
					text,
					additionLineNumber,
					deletionLineNumber,
				});
				additionLineNumber++;
				deletionLineNumber++;
			}
		}
	}

	return {
		id,
		noteKey: id,
		header: "",
		context: "",
		lines,
		changeCount: additionCount + deletionCount,
		// rawPatch is not consumed by ReviewDiffBlock when a hunk is provided.
		rawPatch: "",
		patchStartLine: 0,
		patchLineCount: lines.length,
		additionStart: 1,
		additionCount,
		deletionStart: 1,
		deletionCount,
		collapsedBefore: 0,
	};
}

/**
 * Thin separator row rendered between consecutive edit hunks to evoke a
 * multi-hunk file diff (where unchanged context between hunks is elided).
 * Edits don't include absolute file positions, so we don't show line
 * ranges — just a visual gap with a chevron indicator.
 */
function EditSkipRow() {
	return (
		<box
			paddingX={1}
			backgroundColor={theme.bgMuted}
			height={1}
			flexShrink={0}
			width="100%"
		>
			<text fg={theme.textMuted} bg={theme.bgMuted}>
				{ELLIPSIS}
			</text>
		</box>
	);
}

function EditsBlock(props: {
	path: string;
	edits: Array<{ oldText: string; newText: string }>;
}) {
	const filetype = createMemo(() => inferFiletype(props.path));
	return (
		<box flexDirection="column" gap={0} width="100%">
			<For each={props.edits}>
				{(edit, i) => {
					const hunk = createMemo(() =>
						buildEditHunk(edit.oldText, edit.newText, `edit-${i()}`),
					);
					return (
						<>
							<Show when={i() > 0}>
								<EditSkipRow />
							</Show>
							<ReviewDiffBlock
								view="unified"
								hunk={hunk()}
								filetype={filetype()}
								showLineNumbers={false}
							/>
						</>
					);
				}}
			</For>
		</box>
	);
}

function EnrichedDetailBlock(props: { detail: EnrichedDetail }) {
	return (
		<Switch>
			<Match when={props.detail.kind === "file" && props.detail}>
				{(d) => <FileCodeBlock path={d().path} content={d().content} />}
			</Match>
			<Match when={props.detail.kind === "edits" && props.detail}>
				{(d) => <EditsBlock path={d().path} edits={d().edits} />}
			</Match>
		</Switch>
	);
}

function PendingToolCall(props: {
	tc: ToolCall;
	aborted?: boolean;
	fullArgs?: boolean;
}) {
	const arg = () =>
		toolArgumentPresentation(props.tc, props.tc.arguments, props.fullArgs);
	return (
		<box flexDirection="row" gap={1} width="100%">
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
				{toolDisplayName(props.tc)}
			</text>
			<Show when={arg().text.length > 0}>
				<text
					fg={props.aborted ? theme.textMuted : theme.textPrimary}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
					wrapMode={arg().compact ? "none" : undefined}
					flexShrink={arg().compact ? 1 : undefined}
				>
					{arg().text}
				</text>
			</Show>
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
	fullArgs?: boolean;
	noTruncate?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(false);
	const renderer = useRenderer();
	const lines = () =>
		extractToolProgressLines(props.result ?? props.partialResult ?? null);
	const hasOutput = () => lines().length > 0;
	const displayLines = () => {
		if (!expanded()) return [];
		if (!props.noTruncate && lines().length > 40) {
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
	const arg = () =>
		toolArgumentPresentation(props.tc, toolArgs(), props.fullArgs);
	const hasDetails = () => hasOutput() || arg().detail !== undefined;

	return (
		<box flexDirection="column" gap={0} width="100%">
			<box
				flexDirection="row"
				gap={1}
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					if (hasDetails()) setExpanded(!expanded());
				}}
			>
				<Show when={prefix()} fallback={<InlineSpinner />}>
					{(value) => <text fg={headerColor()}>{value()}</text>}
				</Show>
				<text
					fg={headerColor()}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
				>
					{toolDisplayName(props.tc)}
				</text>
				<Show when={arg().text.length > 0}>
					<text
						fg={props.aborted ? theme.textMuted : theme.textPrimary}
						attributes={props.aborted ? ABORTED_ATTRS : undefined}
						wrapMode={arg().compact ? "none" : undefined}
						flexShrink={arg().compact ? 1 : undefined}
					>
						{arg().text}
					</text>
				</Show>
				<Show when={hasDetails() && !props.aborted}>
					<text fg={theme.metaText} flexShrink={0}>
						{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}
					</text>
				</Show>
			</box>
			<Show when={expanded() ? arg() : undefined}>
				{(expandedArg) => (
					<box paddingLeft={2} flexDirection="column" gap={0} width="100%">
						<BashCommandDetail command={expandedArg().detail} />
						<For each={displayLines()}>
							{(line) => <text fg={theme.textMuted}>{line}</text>}
						</For>
					</box>
				)}
			</Show>
		</box>
	);
}

function CompletedToolCall(props: {
	tc: ToolCall;
	result: ToolResultMessage;
	aborted?: boolean;
	fullArgs?: boolean;
	noTruncate?: boolean;
	enrichOutput?: boolean;
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
	const enrichedDetail = createMemo(() =>
		props.enrichOutput ? detectEnrichment(props.tc, props.result) : null,
	);
	const hasOutput = () => enrichedDetail() !== null || lines.length > 0;
	const arg = toolArgumentPresentation(
		props.tc,
		props.tc.arguments,
		props.fullArgs,
	);
	const hasDetails = () => hasOutput() || arg.detail !== undefined;

	const displayLines = () => {
		if (!expanded()) return [];
		if (!props.noTruncate && lines.length > 40) {
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
					if (hasDetails()) setExpanded(!expanded());
				}}
			>
				<text
					fg={headerColor}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
				>
					{prefix} {toolDisplayName(props.tc)}
				</text>
				<Show when={arg.text.length > 0}>
					<text
						fg={props.aborted ? theme.textMuted : theme.textPrimary}
						attributes={props.aborted ? ABORTED_ATTRS : undefined}
						wrapMode={arg.compact ? "none" : undefined}
						flexShrink={arg.compact ? 1 : undefined}
					>
						{arg.text}
					</text>
				</Show>
				<Show when={hasDetails() && !props.aborted}>
					<text fg={theme.metaText} flexShrink={0}>
						{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}
					</text>
				</Show>
			</box>
			<Show when={expanded() ? arg : undefined}>
				{(expandedArg) => (
					<box paddingLeft={2} flexDirection="column" gap={0} width="100%">
						<BashCommandDetail command={expandedArg().detail} />
						<Show
							when={enrichedDetail()}
							fallback={
								<For each={displayLines()}>
									{(line) => <text fg={theme.textMuted}>{line}</text>}
								</For>
							}
						>
							{(detail) => <EnrichedDetailBlock detail={detail()} />}
						</Show>
					</box>
				)}
			</Show>
		</box>
	);
}

/**
 * Per-tool row dispatcher. Picks Pending, Live, or Completed presentation
 * based on whether a result has landed and whether live progress is
 * available.
 */
export function PerToolRow(props: {
	tc: ToolCall;
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
	fullArgs?: boolean;
	noTruncate?: boolean;
	enrichOutput?: boolean;
}) {
	const result = () => props.toolResults.get(props.tc.id);
	const liveTool = () => props.liveTools[props.tc.id];
	return (
		<Show
			when={result()}
			fallback={
				<Show
					when={liveTool()}
					fallback={
						<PendingToolCall
							tc={props.tc}
							aborted={props.aborted}
							fullArgs={props.fullArgs}
						/>
					}
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
							fullArgs={props.fullArgs}
							noTruncate={props.noTruncate}
						/>
					)}
				</Show>
			}
		>
			{(r) => (
				<CompletedToolCall
					tc={props.tc}
					result={r()}
					aborted={props.aborted}
					fullArgs={props.fullArgs}
					noTruncate={props.noTruncate}
					enrichOutput={props.enrichOutput}
				/>
			)}
		</Show>
	);
}

/**
 * Flat assistant rendering for use inside the turn activity view.
 * Renders prose followed by each tool call as a plain collapsed row, so
 * the activity list reads like compact log lines.
 */
export function FlatAssistantEntry(props: {
	msg: AssistantMessage;
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
	fullArgs?: boolean;
	noTruncate?: boolean;
	enrichOutput?: boolean;
}) {
	if (isAssistantError(props.msg)) {
		return <text fg={theme.errorText}>{props.msg.errorMessage}</text>;
	}

	// `parts` is reactive so an in-flight assistant message can grow its tool
	// calls without unmounting this entry. A shallow equality check on the
	// derived value prevents spurious downstream notifications when an
	// upstream tick fires but the message's text + tool-call refs are
	// unchanged — keeping the inner For and per-row state quiet.
	const parts = createMemo(() => extractAssistantParts(props.msg), undefined, {
		equals: (prev, next) =>
			prev.text === next.text &&
			prev.toolCalls.length === next.toolCalls.length &&
			prev.toolCalls.every((tc, i) => tc === next.toolCalls[i]),
	});
	const text = () => parts().text;
	const toolCalls = () => parts().toolCalls;
	const hasText = () => text().length > 0;
	const hasTools = () => toolCalls().length > 0;

	return (
		<box
			flexDirection="column"
			gap={hasText() && hasTools() ? 1 : 0}
			width="100%"
		>
			<Show when={hasText()}>
				<KitMarkdown
					content={text()}
					fg={props.aborted ? theme.textMuted : theme.textPrimary}
				/>
			</Show>
			<Show when={hasTools()}>
				<box flexDirection="column" gap={0}>
					<For each={toolCalls()}>
						{(tc) => (
							<PerToolRow
								tc={tc}
								toolResults={props.toolResults}
								liveTools={props.liveTools}
								aborted={props.aborted}
								fullArgs={props.fullArgs}
								noTruncate={props.noTruncate}
								enrichOutput={props.enrichOutput}
							/>
						)}
					</For>
				</box>
			</Show>
		</box>
	);
}

/**
 * Drawer chip for tool calls in a single assistant message. Clicking opens
 * the turn activity panel with the message's prose + tool details, kept
 * live via the runtime.
 */
export function ToolDrawer(props: {
	itemId: string;
	toolCalls: ToolCall[];
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
	runtime: AgentRuntime;
	openActivity: OpenActivity;
}) {
	function openActivity() {
		props.openActivity({ kind: "single-item", itemId: props.itemId });
	}

	return (
		<DrawerChip
			toolCalls={props.toolCalls}
			toolResults={props.toolResults}
			aborted={props.aborted}
			onActivate={openActivity}
		/>
	);
}

export function AssistantEntry(props: {
	itemId: string;
	msg: AssistantMessage;
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
	runtime: AgentRuntime;
	openActivity: OpenActivity;
	openMessageContextMenu: OpenMessageContextMenu;
}) {
	const messageContextMenuGesture = createMessageContextMenuGesture(
		() =>
			isAssistantError(props.msg)
				? (props.msg.errorMessage ?? "")
				: extractAssistantMarkdownSource(props.msg),
		props.openMessageContextMenu,
	);

	if (isAssistantError(props.msg)) {
		return (
			<box
				paddingLeft={1}
				flexDirection="column"
				gap={0}
				width="100%"
				{...messageContextMenuGesture}
			>
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
			{...messageContextMenuGesture}
		>
			<Show when={hasToolCalls}>
				<ToolDrawer
					itemId={props.itemId}
					toolCalls={toolCalls}
					toolResults={props.toolResults}
					liveTools={props.liveTools}
					aborted={props.aborted}
					runtime={props.runtime}
					openActivity={props.openActivity}
				/>
			</Show>
			<Show when={hasText}>
				<KitMarkdown
					content={text}
					fg={props.aborted ? theme.textMuted : theme.textPrimary}
				/>
			</Show>
		</box>
	);
}
