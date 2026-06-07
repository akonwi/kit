import { TextAttributes } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { diffLines } from "diff";
import type { JSX } from "solid-js";
import { createMemo, createSignal, For, Match, Show, Switch } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { ReviewHunk, ReviewLine } from "../../features/review/model";
import { ReviewDiffBlock } from "../../features/review/ReviewDiffBlock";
import type {
	AssistantMessage,
	ToolCall,
	ToolResultMessage,
} from "../../runtime/agent";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { inferFiletype } from "../filetype";
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
import { DialogCard } from "./dialog-card";
import { DrawerChip } from "./drawer-chip";
import { InlineSpinner } from "./inline-spinner";
import { TurnActivityDialog } from "./TurnActivityDialog";
import {
	extractAssistantParts,
	extractToolResultLines,
	formatToolArgs,
	isAssistantError,
} from "./turns";
import type { OpenOverlay } from "./types";

const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;

function toolAccentColor(toolName: string): string {
	return toolName === "subagent" ? theme.subagentText : theme.toolText;
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
	const filetype = createMemo(() => inferFiletype(props.path));
	return (
		<Show
			when={filetype()}
			fallback={
				<For each={props.content.split("\n")}>
					{(line) => <text fg={theme.textMuted}>{line}</text>}
				</For>
			}
		>
			{(ft) => (
				<code
					filetype={ft()}
					content={props.content}
					syntaxStyle={syntaxStyle()}
				/>
			)}
		</Show>
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

function EditsBlock(props: {
	path: string;
	edits: Array<{ oldText: string; newText: string }>;
}) {
	const filetype = createMemo(() => inferFiletype(props.path));
	return (
		<box flexDirection="column" gap={1} width="100%">
			<For each={props.edits}>
				{(edit, i) => {
					const hunk = createMemo(() =>
						buildEditHunk(edit.oldText, edit.newText, `edit-${i()}`),
					);
					return (
						<box flexDirection="column" gap={0} width="100%">
							<Show when={props.edits.length > 1}>
								<text fg={theme.textMuted}>edit {i() + 1}</text>
							</Show>
							<ReviewDiffBlock
								view="unified"
								hunk={hunk()}
								filetype={filetype()}
							/>
						</box>
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
	const argText = () =>
		formatToolArgs(props.tc.arguments, { full: props.fullArgs }).trimStart();
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
			</text>
			<Show when={argText().length > 0}>
				<text
					fg={props.aborted ? theme.textMuted : theme.textPrimary}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
				>
					{argText()}
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
	autoExpand?: boolean;
	fullArgs?: boolean;
	noTruncate?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(props.autoExpand ?? false);
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
	const liveArgText = () =>
		formatToolArgs(toolArgs(), { full: props.fullArgs }).trimStart();

	return (
		<box flexDirection="column" gap={0} width="100%">
			<box
				flexDirection="row"
				gap={1}
				paddingY={1}
				border={expanded() ? ["bottom"] : false}
				borderColor={theme.bgAccent}
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
				</text>
				<Show when={liveArgText().length > 0}>
					<text
						fg={props.aborted ? theme.textMuted : theme.textPrimary}
						attributes={props.aborted ? ABORTED_ATTRS : undefined}
					>
						{liveArgText()}
					</text>
				</Show>
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
	autoExpand?: boolean;
	fullArgs?: boolean;
	noTruncate?: boolean;
	enrichOutput?: boolean;
}) {
	const [expanded, setExpanded] = createSignal(props.autoExpand ?? false);
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
	const argText = formatToolArgs(props.tc.arguments, {
		full: props.fullArgs,
	}).trimStart();

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
				paddingY={1}
				border={expanded() ? ["bottom"] : false}
				borderColor={theme.bgAccent}
				onMouseDown={() => {
					if (renderer.getSelection()?.getSelectedText()) return;
					if (hasOutput()) setExpanded(!expanded());
				}}
			>
				<text
					fg={headerColor}
					attributes={props.aborted ? ABORTED_ATTRS : undefined}
				>
					{prefix} {props.tc.name}
				</text>
				<Show when={argText.length > 0}>
					<text
						fg={props.aborted ? theme.textMuted : theme.textPrimary}
						attributes={props.aborted ? ABORTED_ATTRS : undefined}
					>
						{argText}
					</text>
				</Show>
				<Show when={hasOutput() && !props.aborted}>
					<text fg={theme.metaText}>
						{expanded() ? TRIANGLE_DOWN : TRIANGLE_RIGHT}
					</text>
				</Show>
			</box>
			<Show when={expanded()}>
				<box paddingLeft={2} flexDirection="column" gap={0} width="100%">
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
	autoExpand?: boolean;
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
							autoExpand={props.autoExpand}
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
					autoExpand={props.autoExpand}
					fullArgs={props.fullArgs}
					noTruncate={props.noTruncate}
					enrichOutput={props.enrichOutput}
				/>
			)}
		</Show>
	);
}

/**
 * Flat assistant rendering for use inside the turn activity dialog.
 * Renders prose followed by each tool call wrapped in a DialogCard for
 * surface discrimination.
 */
export function FlatAssistantEntry(props: {
	msg: AssistantMessage;
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
	autoExpand?: boolean;
	fullArgs?: boolean;
	noTruncate?: boolean;
	enrichOutput?: boolean;
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
				<box flexDirection="column" gap={1}>
					<For each={toolCalls}>
						{(tc) => (
							<DialogCard>
								<PerToolRow
									tc={tc}
									toolResults={props.toolResults}
									liveTools={props.liveTools}
									aborted={props.aborted}
									autoExpand={props.autoExpand}
									fullArgs={props.fullArgs}
									noTruncate={props.noTruncate}
									enrichOutput={props.enrichOutput}
								/>
							</DialogCard>
						)}
					</For>
				</box>
			</Show>
		</box>
	);
}

/**
 * Drawer chip for tool calls in a single assistant message. Clicking opens
 * the turn activity dialog with the message's prose + tool details, kept
 * live via the runtime.
 */
export function ToolDrawer(props: {
	itemId: string;
	toolCalls: ToolCall[];
	toolResults: Map<string, ToolResultMessage>;
	liveTools: LiveToolsForTurn;
	aborted?: boolean;
	runtime: AgentRuntime;
	openOverlay: OpenOverlay;
}) {
	function openDialog() {
		const itemId = props.itemId;
		const runtime = props.runtime;
		void props.openOverlay(
			(overlayProps: OverlayComponentProps<unknown>): JSX.Element => (
				<TurnActivityDialog
					runtime={runtime}
					source={{ kind: "single-item", itemId }}
					done={overlayProps.done}
					surfaceProps={overlayProps.surfaceProps}
					active={overlayProps.active}
				/>
			),
		);
	}

	return (
		<DrawerChip
			toolCalls={props.toolCalls}
			toolResults={props.toolResults}
			aborted={props.aborted}
			onActivate={openDialog}
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
	openOverlay: OpenOverlay;
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
					itemId={props.itemId}
					toolCalls={toolCalls}
					toolResults={props.toolResults}
					liveTools={props.liveTools}
					aborted={props.aborted}
					runtime={props.runtime}
					openOverlay={props.openOverlay}
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
