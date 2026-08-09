/** @jsxImportSource solid-js */
import { createMemo, For, type JSX, Match, Show, Switch } from "solid-js";
import type { ToolResultMessage } from "../runtime/agent";
import type { DisplayItem, TranscriptItem } from "../shell/transcript/turns";
import {
	extractAssistantParts,
	isAssistantError,
} from "../shell/transcript/turns";
import type { ToolActivity } from "./client-state";
import { SafeMarkdown } from "./SafeMarkdown";
import { mergeLiveToolCalls, ToolDrawer, ToolRows } from "./ToolDrawer";
import { UserEntry } from "./UserEntry";

export function AssistantEntry(props: {
	item: Extract<TranscriptItem, { kind: "assistant" }>;
	liveTools: ToolActivity[];
}): JSX.Element {
	const parts = createMemo(() => extractAssistantParts(props.item.message));
	const error = createMemo(() =>
		isAssistantError(props.item.message)
			? (props.item.message.errorMessage ?? "The model reported an error.")
			: "",
	);
	const itemLiveTools = createMemo(() => {
		const ids = new Set(parts().toolCalls.map((toolCall) => toolCall.id));
		return props.liveTools.filter((tool) => ids.has(tool.id));
	});

	return (
		<article
			class="transcript-entry assistant-entry"
			classList={{
				"is-aborted": props.item.aborted,
				"is-error": error().length > 0,
			}}
		>
			<span data-visually-hidden>
				Kit: {props.item.aborted ? "aborted. " : ""}
			</span>
			<Show when={error()}>
				{(message) => <div class="assistant-error">{message()}</div>}
			</Show>
			<Show when={parts().text.trim()}>
				<SafeMarkdown content={parts().text} />
			</Show>
			<Show when={parts().toolCalls.length > 0}>
				<ToolDrawer
					toolCalls={parts().toolCalls}
					toolResults={props.item.toolResults}
					liveTools={itemLiveTools()}
					aborted={props.item.aborted}
				/>
			</Show>
		</article>
	);
}

function TurnWorkStep(props: {
	item: () => Extract<TranscriptItem, { kind: "assistant" }> | undefined;
	liveTools: ToolActivity[];
}): JSX.Element {
	const parts = createMemo(() => {
		const item = props.item();
		return item
			? extractAssistantParts(item.message)
			: { text: "", toolCalls: [] };
	});
	const itemLiveTools = createMemo(() => {
		const ids = new Set(parts().toolCalls.map((toolCall) => toolCall.id));
		return props.liveTools.filter((tool) => ids.has(tool.id));
	});
	return (
		<div class="turn-work-step">
			<Show when={parts().text.trim()}>
				<SafeMarkdown
					class="markdown-content intermediate-prose"
					content={parts().text}
				/>
			</Show>
			<Show when={props.item()}>
				{(item) => (
					<ToolRows
						toolCalls={parts().toolCalls}
						toolResults={item().toolResults}
						liveTools={itemLiveTools()}
						aborted={item().aborted}
					/>
				)}
			</Show>
		</div>
	);
}

function TurnWorkEntry(props: {
	items: TranscriptItem[];
	liveTools: ToolActivity[];
}): JSX.Element {
	const assistantItems = createMemo(() =>
		props.items.filter(
			(item): item is Extract<TranscriptItem, { kind: "assistant" }> =>
				item.kind === "assistant",
		),
	);
	const assistantItemIds = createMemo(() =>
		assistantItems().map((item) => item.id),
	);
	const assistantItemsById = createMemo(
		() => new Map(assistantItems().map((item) => [item.id, item])),
	);
	const persistedToolCalls = createMemo(() =>
		assistantItems().flatMap(
			(item) => extractAssistantParts(item.message).toolCalls,
		),
	);
	const toolCalls = createMemo(() =>
		mergeLiveToolCalls(persistedToolCalls(), props.liveTools),
	);
	const persistedToolCallIds = createMemo(
		() => new Set(persistedToolCalls().map((toolCall) => toolCall.id)),
	);
	const liveOnlyToolCalls = createMemo(() =>
		toolCalls().filter((toolCall) => !persistedToolCallIds().has(toolCall.id)),
	);
	const toolResults = createMemo(() => {
		const merged = new Map<string, ToolResultMessage>();
		for (const item of assistantItems()) {
			for (const [id, result] of item.toolResults) merged.set(id, result);
		}
		return merged;
	});
	const aborted = createMemo(() =>
		assistantItems().some((item) => item.aborted),
	);
	const stepLabel = createMemo(
		() => `${props.items.length} step${props.items.length === 1 ? "" : "s"}`,
	);

	return (
		<div class="transcript-entry turn-work-entry">
			<ToolDrawer
				toolCalls={toolCalls()}
				toolResults={toolResults()}
				liveTools={props.liveTools}
				aborted={aborted()}
				emptyLabel={stepLabel()}
			>
				<For each={assistantItemIds()}>
					{(id) => (
						<TurnWorkStep
							item={() => assistantItemsById().get(id)}
							liveTools={props.liveTools}
						/>
					)}
				</For>
				<Show when={liveOnlyToolCalls().length > 0}>
					<ToolRows
						toolCalls={liveOnlyToolCalls()}
						toolResults={toolResults()}
						liveTools={props.liveTools}
						aborted={aborted()}
					/>
				</Show>
			</ToolDrawer>
		</div>
	);
}

function HandoffSummaryEntry(props: {
	item: Extract<TranscriptItem, { kind: "handoff-summary" }>;
}): JSX.Element {
	const text = () => extractAssistantParts(props.item.message).text;
	const source = () => props.item.message.synthetic?.sourceSessionName?.trim();
	return (
		<details
			class="transcript-entry handoff-summary-entry"
			classList={{ "is-aborted": props.item.aborted }}
		>
			<summary>
				<span class="disclosure-marker" aria-hidden="true" />
				<span class="handoff-divider" aria-hidden="true" />
				<span>
					merged handoff summary
					<Show when={source()}>{(value) => <> · {value()}</>}</Show>
				</span>
				<span class="handoff-divider" aria-hidden="true" />
			</summary>
			<SafeMarkdown content={text()} />
		</details>
	);
}

function BashEntry(props: {
	item: Extract<TranscriptItem, { kind: "bash" }>;
}): JSX.Element {
	const outputLines = () => (props.item.message.output ?? "").split("\n");
	const output = () => {
		const lines = outputLines();
		if (lines.length <= 20) return lines.join("\n");
		return `${lines.slice(0, 18).join("\n")}\n… (${lines.length - 18} more lines)`;
	};
	const pending = () =>
		props.item.message.exitCode === undefined && !props.item.message.cancelled;
	const failed = () =>
		!pending() &&
		!props.item.message.cancelled &&
		props.item.message.exitCode !== 0;
	const status = () =>
		pending()
			? "running"
			: props.item.message.cancelled
				? "aborted"
				: failed()
					? "failed"
					: "completed";
	return (
		<details class="transcript-entry bash-entry" open>
			<summary>
				<span class="disclosure-marker" aria-hidden="true" />
				<span
					class="tool-state"
					data-status={
						pending()
							? "running"
							: props.item.message.cancelled
								? "aborted"
								: "complete"
					}
					data-error={String(failed())}
					aria-hidden="true"
				/>
				<span data-visually-hidden>{status()}: </span>
				<code>{props.item.message.command}</code>
			</summary>
			<Show when={output().trim()}>
				<pre class="tool-output">{output()}</pre>
			</Show>
		</details>
	);
}

export function TurnEntry(props: {
	displayItem: () => DisplayItem | undefined;
	liveTools: ToolActivity[];
}): JSX.Element {
	const turnWork = createMemo(() => {
		const item = props.displayItem();
		return item?.kind === "turn-work" ? item : null;
	});
	const singleItem = createMemo(() => {
		const item = props.displayItem();
		return item?.kind === "single" ? item.item : null;
	});
	const userItem = createMemo(() => {
		const item = singleItem();
		return item?.kind === "user" ? item : null;
	});
	const assistantItem = createMemo(() => {
		const item = singleItem();
		return item?.kind === "assistant" ? item : null;
	});
	const handoffItem = createMemo(() => {
		const item = singleItem();
		return item?.kind === "handoff-summary" ? item : null;
	});
	const bashItem = createMemo(() => {
		const item = singleItem();
		return item?.kind === "bash" ? item : null;
	});

	return (
		<Switch>
			<Match when={turnWork()}>
				{(item) => (
					<TurnWorkEntry items={item().items} liveTools={props.liveTools} />
				)}
			</Match>
			<Match when={userItem()}>{(item) => <UserEntry item={item()} />}</Match>
			<Match when={assistantItem()}>
				{(item) => <AssistantEntry item={item()} liveTools={props.liveTools} />}
			</Match>
			<Match when={handoffItem()}>
				{(item) => <HandoffSummaryEntry item={item()} />}
			</Match>
			<Match when={bashItem()}>{(item) => <BashEntry item={item()} />}</Match>
		</Switch>
	);
}
