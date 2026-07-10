import {
	createEffect,
	createMemo,
	createSignal,
	For,
	onCleanup,
	Show,
} from "solid-js";
import { HEAVY_LINE } from "../glyphs";
import { scrollbarStyle, theme } from "../theme";
import {
	type LiveToolExecutionMap,
	reconcileLiveTools,
	upsertLiveTool,
} from "../transcript-live-tools";
import { TurnEntry } from "./turn-entry";
import { type DisplayItem, groupItemsForDisplay } from "./turns";
import type { TranscriptPaneProps } from "./types";

export type { TranscriptPaneProps } from "./types";

export function TranscriptPane(props: TranscriptPaneProps) {
	const [liveTools, setLiveTools] = createSignal<LiveToolExecutionMap>({});
	// Track the turn that is currently streaming so its intermediate work folds
	// into a single growing drawer instead of expanding into per-message rows
	// and tool drawers that visibly restructure as each message ends.
	const [inProgressTurnId, setInProgressTurnId] = createSignal<string | null>(
		(() => {
			if (!props.runtime.getStatus().isStreaming) return null;
			return props.runtime.getTurns().at(-1)?.id ?? null;
		})(),
	);
	const displayItems = createMemo<DisplayItem[]>((previous) =>
		groupItemsForDisplay(props.items, inProgressTurnId(), previous),
	);

	createEffect(() => {
		setLiveTools((prev) => reconcileLiveTools(prev, props.items));
	});

	const unsubscribeTurnStarted = props.runtime.subscribe(
		"agent.turn.started",
		(event) => {
			setInProgressTurnId(event.turn.id);
		},
	);
	const unsubscribeTurnCompleted = props.runtime.subscribe(
		"agent.turn.completed",
		() => {
			setInProgressTurnId(null);
		},
	);
	const unsubscribeSessionChanged = props.runtime.subscribe(
		"session.active.changed",
		() => {
			setInProgressTurnId(null);
		},
	);

	const unsubscribeStarted = props.runtime.subscribe(
		"agent.tool.started",
		(event) => {
			setLiveTools((prev) =>
				upsertLiveTool(prev, {
					turnId: event.turn.id,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					partialResult: null,
					result: null,
					isError: null,
					state: "started",
				}),
			);
		},
	);
	const unsubscribeUpdated = props.runtime.subscribe(
		"agent.tool.updated",
		(event) => {
			setLiveTools((prev) => {
				const existing = prev[event.turn.id]?.[event.toolCallId] ?? null;
				return upsertLiveTool(prev, {
					turnId: event.turn.id,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					partialResult: event.partialResult,
					result: existing?.result ?? null,
					isError: existing?.isError ?? null,
					state: "updated",
				});
			});
		},
	);
	const unsubscribeEnded = props.runtime.subscribe(
		"agent.tool.ended",
		(event) => {
			setLiveTools((prev) => {
				const existing = prev[event.turn.id]?.[event.toolCallId] ?? null;
				return upsertLiveTool(prev, {
					turnId: event.turn.id,
					toolCallId: event.toolCallId,
					toolName: event.toolName,
					args: event.args,
					partialResult: existing?.partialResult ?? null,
					result: event.result,
					isError: event.isError,
					state: "ended",
				});
			});
		},
	);

	onCleanup(() => {
		unsubscribeStarted();
		unsubscribeUpdated();
		unsubscribeEnded();
		unsubscribeTurnStarted();
		unsubscribeTurnCompleted();
		unsubscribeSessionChanged();
	});

	return (
		<scrollbox
			flexGrow={1}
			height="100%"
			scrollY
			stickyStart="bottom"
			stickyScroll
			padding={1}
			style={scrollbarStyle()}
		>
			<Show
				when={props.items.length > 0}
				fallback={
					<box
						flexGrow={1}
						flexDirection="column"
						justifyContent="center"
						alignItems="center"
						gap={1}
						width="100%"
					>
						<box flexDirection="column" alignItems="center" gap={0}>
							<text fg={theme.textPrimary}>k i t</text>
							<text fg={theme.borderAccent}>{HEAVY_LINE.repeat(11)}</text>
						</box>
						<text fg={theme.textSecondary}>Ask a question or give a task.</text>
						<text fg={theme.textPlaceholder}>/ to open commands</text>
					</box>
				}
			>
				<box flexDirection="column" gap={0} width="100%">
					<For each={displayItems()}>
						{(displayItem, index) => {
							const turnId =
								displayItem.kind === "single"
									? displayItem.item.turnId
									: displayItem.turnId;
							const isUser =
								displayItem.kind === "single" &&
								displayItem.item.kind === "user";
							// Extra spacing before user messages for visual separation
							const spacerHeight = () => {
								if (index() === 0) return 0;
								if (isUser) return 2;
								return 1;
							};
							return (
								<>
									<Show when={spacerHeight() > 0}>
										<box height={spacerHeight()} />
									</Show>
									<TurnEntry
										displayItem={displayItem}
										liveTools={liveTools()[turnId] ?? {}}
										showToast={props.showToast}
										runtime={props.runtime}
										openActivity={props.openActivity}
										openReviewAttachment={props.openReviewAttachment}
									/>
								</>
							);
						}}
					</For>
				</box>
			</Show>
		</scrollbox>
	);
}
