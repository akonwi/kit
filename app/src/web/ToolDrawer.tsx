/** @jsxImportSource solid-js */

import type { ToolActivity } from "@akonwi/kit-session-client";
import { createMemo, For, type JSX, Show } from "solid-js";
import type { ToolCall, ToolResultMessage } from "../runtime/agent";
import {
	extractToolResultLines,
	formatToolArgs,
	toolArgKeys,
	toolDisplayName,
} from "../shell/transcript/turns";
import { extractToolProgressLines } from "../shell/transcript-live-tools";
import { BrailleSpinner } from "./BrailleSpinner";
import { displayValue } from "./presentation";

const MAX_VISIBLE_TOOL_NAMES = 8;

export function mergeLiveToolCalls(
	toolCalls: ToolCall[],
	liveTools: ToolActivity[],
): ToolCall[] {
	const merged = [...toolCalls];
	const ids = new Set(toolCalls.map((toolCall) => toolCall.id));
	for (const tool of liveTools) {
		if (ids.has(tool.id)) continue;
		merged.push({
			type: "toolCall",
			id: tool.id,
			name: tool.name,
			arguments: tool.args ?? {},
		});
	}
	return merged;
}

function toolOutput(
	result: ToolResultMessage | undefined,
	live: ToolActivity | undefined,
): string {
	if (result) return extractToolResultLines(result).join("\n");
	const liveResult = live?.result ?? live?.partialResult;
	const progressLines = extractToolProgressLines(liveResult);
	return progressLines.length > 0
		? progressLines.join("\n")
		: displayValue(liveResult);
}

function ToolRow(props: {
	toolCall: ToolCall;
	result?: ToolResultMessage;
	live?: ToolActivity;
	aborted?: boolean;
}): JSX.Element {
	const output = createMemo(() => toolOutput(props.result, props.live));
	const complete = createMemo(
		() => props.result !== undefined || props.live?.status === "complete",
	);
	const isError = createMemo(
		() => props.result?.isError === true || props.live?.isError === true,
	);
	const args = createMemo(() => {
		const source = props.live?.args ?? props.toolCall.arguments;
		return formatToolArgs(source, { keys: toolArgKeys(props.toolCall) }).trim();
	});
	const stateLabel = createMemo(() =>
		props.aborted
			? "aborted"
			: !complete()
				? "running"
				: isError()
					? "failed"
					: "completed",
	);
	const header = (
		<>
			<Show
				when={!props.aborted && !complete()}
				fallback={
					<span
						class="tool-state"
						data-status={props.aborted ? "aborted" : "complete"}
						data-error={String(isError())}
						aria-hidden="true"
					/>
				}
			>
				<BrailleSpinner class="tool-state" />
			</Show>
			<span data-visually-hidden>{stateLabel()}: </span>
			<span class="tool-name">{toolDisplayName(props.toolCall)}</span>
			<Show when={args()}>
				{(value) => <span class="tool-args">{value()}</span>}
			</Show>
		</>
	);

	return (
		<Show
			when={output()}
			fallback={<div class="tool-row tool-row-static">{header}</div>}
		>
			<details class="tool-row" data-error={String(isError())}>
				<summary>
					<span class="disclosure-marker" aria-hidden="true" />
					{header}
				</summary>
				<pre class="tool-output">{output()}</pre>
			</details>
		</Show>
	);
}

export function ToolRows(props: {
	toolCalls: ToolCall[];
	toolResults: Map<string, ToolResultMessage>;
	liveTools: ToolActivity[];
	aborted?: boolean;
}): JSX.Element {
	const toolCallIds = createMemo(() =>
		props.toolCalls.map((toolCall) => toolCall.id),
	);
	const toolCallsById = createMemo(
		() => new Map(props.toolCalls.map((toolCall) => [toolCall.id, toolCall])),
	);
	return (
		<For each={toolCallIds()}>
			{(id) => (
				<Show when={toolCallsById().get(id)}>
					{(toolCall) => (
						<ToolRow
							toolCall={toolCall()}
							result={props.toolResults.get(id)}
							live={props.liveTools.find((tool) => tool.id === id)}
							aborted={props.aborted}
						/>
					)}
				</Show>
			)}
		</For>
	);
}

export function ToolDrawer(props: {
	toolCalls: ToolCall[];
	toolResults: Map<string, ToolResultMessage>;
	liveTools: ToolActivity[];
	aborted?: boolean;
	emptyLabel?: string;
	active?: boolean;
	onActivate: () => void;
}): JSX.Element {
	const toolCalls = createMemo(() => props.toolCalls);
	const running = createMemo(
		() =>
			!props.aborted &&
			toolCalls().some(
				(toolCall) =>
					!props.toolResults.has(toolCall.id) &&
					props.liveTools.find((tool) => tool.id === toolCall.id)?.status !==
						"complete",
			),
	);
	const countLabel = createMemo(() => {
		if (toolCalls().length === 0 && props.emptyLabel) return props.emptyLabel;
		return `${toolCalls().length} tool call${toolCalls().length === 1 ? "" : "s"}`;
	});
	const visibleNames = createMemo(() =>
		toolCalls().slice(0, MAX_VISIBLE_TOOL_NAMES),
	);
	const overflowCount = createMemo(() =>
		Math.max(0, toolCalls().length - MAX_VISIBLE_TOOL_NAMES),
	);
	const SummaryContent = () => (
		<>
			<span class="panel-launch-marker" aria-hidden="true" />
			<Show
				when={running()}
				fallback={
					<span
						class="drawer-state"
						data-status="complete"
						aria-hidden="true"
					/>
				}
			>
				<BrailleSpinner class="drawer-state" />
			</Show>
			<span data-visually-hidden>
				{props.aborted ? "aborted: " : running() ? "running: " : "completed: "}
			</span>
			<span class="drawer-count">{countLabel()}</span>
			<Show when={visibleNames().length > 0}>
				<span class="drawer-tool-names">
					{" · "}
					<For each={visibleNames()}>
						{(toolCall, index) => (
							<>
								<Show when={index() > 0}> · </Show>
								<span>{toolDisplayName(toolCall)}</span>
							</>
						)}
					</For>
					<Show when={overflowCount() > 0}>
						{(count) => <> · +{count()} more</>}
					</Show>
				</span>
			</Show>
			<span data-visually-hidden> — opens activity panel</span>
		</>
	);

	return (
		<div class="tool-drawer">
			<button
				class="tool-drawer-trigger"
				type="button"
				data-variant="ghost"
				aria-controls="workspace-secondary"
				aria-expanded={props.active === true}
				onClick={props.onActivate}
			>
				<SummaryContent />
			</button>
		</div>
	);
}
