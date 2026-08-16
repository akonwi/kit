/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	For,
	type JSX,
	on,
	onCleanup,
	Show,
} from "solid-js";
import type { ToolResultMessage } from "../runtime/agent";
import {
	extractAssistantParts,
	findTurnWorkItems,
	type TranscriptItem,
} from "../shell/transcript/turns";
import type { ToolActivity } from "./client-state";
import { SafeMarkdown } from "./SafeMarkdown";
import { mergeLiveToolCalls, ToolRows } from "./ToolDrawer";
import { liveToolsForTranscriptItems } from "./transcript-model";
import { useWebClient } from "./WebClientContext";
import type { ActivitySource } from "./workspace-panes";

const EMPTY_TOOL_RESULTS = new Map<string, ToolResultMessage>();

function ActivityItem(props: {
	item: TranscriptItem;
	liveTools: ToolActivity[];
}): JSX.Element {
	if (props.item.kind === "assistant") {
		const parts = createMemo(() =>
			props.item.kind === "assistant"
				? extractAssistantParts(props.item.message)
				: { text: "", toolCalls: [] },
		);
		const ids = createMemo(
			() => new Set(parts().toolCalls.map((toolCall) => toolCall.id)),
		);
		return (
			<section class="activity-step">
				<Show when={parts().text.trim()}>
					<SafeMarkdown
						class="markdown-content intermediate-prose"
						content={parts().text}
					/>
				</Show>
				<ToolRows
					toolCalls={parts().toolCalls}
					toolResults={props.item.toolResults}
					liveTools={props.liveTools.filter((tool) => ids().has(tool.id))}
					aborted={props.item.aborted}
				/>
			</section>
		);
	}
	if (props.item.kind === "handoff-summary") {
		return (
			<section class="activity-step">
				<div class="activity-step-label">Merged handoff summary</div>
				<SafeMarkdown
					content={extractAssistantParts(props.item.message).text}
				/>
			</section>
		);
	}
	if (props.item.kind === "bash") {
		return (
			<section class="activity-step">
				<code>{props.item.message.command}</code>
				<Show when={props.item.message.output?.trim()}>
					<pre class="tool-output">{props.item.message.output}</pre>
				</Show>
			</section>
		);
	}
	return null;
}

export function TurnActivityPanel(props: {
	source: ActivitySource;
	active: boolean;
	onUnavailable: () => void;
}): JSX.Element {
	const { snapshot, transcriptItems } = useWebClient();
	let panel: HTMLElement | undefined;
	let body: HTMLDivElement | undefined;
	let followLive = false;
	let scrollFrame: number | null = null;
	const protocol = createMemo(() => snapshot().protocol);
	const items = createMemo(() => {
		const source = props.source;
		if (source.kind === "single-item") {
			const item = transcriptItems().find(
				(candidate) => candidate.id === source.itemId,
			);
			return item ? [item] : [];
		}
		return findTurnWorkItems(
			transcriptItems(),
			source.turnId,
			protocol().activeTurnId,
			source.anchorItemId,
		);
	});
	const persistedToolCalls = createMemo(() =>
		items().flatMap((item) =>
			item.kind === "assistant"
				? extractAssistantParts(item.message).toolCalls
				: [],
		),
	);
	const persistedToolCallIds = createMemo(
		() => new Set(persistedToolCalls().map((toolCall) => toolCall.id)),
	);
	const liveTools = createMemo(() =>
		liveToolsForTranscriptItems(
			items(),
			transcriptItems(),
			protocol().tools,
			protocol().activeTurnId,
		),
	);
	const allToolCalls = createMemo(() =>
		mergeLiveToolCalls(persistedToolCalls(), liveTools()),
	);
	const liveOnlyToolCalls = createMemo(() =>
		allToolCalls().filter(
			(toolCall) => !persistedToolCallIds().has(toolCall.id),
		),
	);
	const itemIds = createMemo(() => items().map((item) => item.id));
	const itemsById = createMemo(
		() => new Map(items().map((item) => [item.id, item])),
	);
	const sourceKey = createMemo(() =>
		props.source.kind === "single-item"
			? `single:${props.source.itemId}`
			: `turn:${props.source.turnId}:${props.source.anchorItemId}`,
	);
	const meta = createMemo(() => {
		const toolCount = allToolCalls().length;
		const stepCount = items().length;
		return `${toolCount} tool call${toolCount === 1 ? "" : "s"} · ${stepCount} step${stepCount === 1 ? "" : "s"}`;
	});

	function scheduleLiveScroll(): void {
		if (
			!followLive ||
			protocol().activeTurnId !== props.source.turnId ||
			!body ||
			scrollFrame !== null
		) {
			return;
		}
		scrollFrame = requestAnimationFrame(() => {
			scrollFrame = null;
			if (followLive && body) body.scrollTop = body.scrollHeight;
		});
	}

	createEffect(
		on(sourceKey, () => {
			followLive = protocol().activeTurnId === props.source.turnId;
			queueMicrotask(() => {
				if (props.active) panel?.focus();
				if (!body) return;
				body.scrollTop = followLive ? body.scrollHeight : 0;
			});
		}),
	);
	createEffect(
		on([() => items().at(-1), () => liveTools().at(-1)], scheduleLiveScroll),
	);
	createEffect(() => {
		if (protocol().activeTurnId !== props.source.turnId) followLive = false;
	});
	createEffect(() => {
		if (items().length > 0 || liveTools().length > 0) return;
		queueMicrotask(props.onUnavailable);
	});
	onCleanup(() => {
		if (scrollFrame !== null) cancelAnimationFrame(scrollFrame);
	});

	return (
		<section ref={panel} class="turn-activity-panel" tabIndex={-1}>
			<header class="workspace-panel-context">
				<div class="activity-panel-meta">{meta()}</div>
			</header>
			<div
				ref={body}
				class="activity-panel-body"
				onScroll={() => {
					if (!body || protocol().activeTurnId !== props.source.turnId) return;
					followLive =
						body.scrollHeight - body.scrollTop - body.clientHeight < 48;
				}}
			>
				<Show
					when={itemIds().length > 0 || liveOnlyToolCalls().length > 0}
					fallback={<div class="activity-empty">Nothing to show here yet</div>}
				>
					<For each={itemIds()}>
						{(id) => (
							<Show when={itemsById().get(id)}>
								{(item) => (
									<ActivityItem item={item()} liveTools={liveTools()} />
								)}
							</Show>
						)}
					</For>
					<Show when={liveOnlyToolCalls().length > 0}>
						<ToolRows
							toolCalls={liveOnlyToolCalls()}
							toolResults={EMPTY_TOOL_RESULTS}
							liveTools={liveTools()}
						/>
					</Show>
				</Show>
			</div>
			<footer class="activity-panel-footer">Esc transcript</footer>
		</section>
	);
}
