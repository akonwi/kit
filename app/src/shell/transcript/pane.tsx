import "../../runtime/custom-messages";
import { createEffect, createSignal, For, onCleanup, Show } from "solid-js";
import { HEAVY_LINE } from "../glyphs";
import { theme } from "../theme";
import {
	type LiveToolExecutionMap,
	reconcileLiveTools,
	upsertLiveTool,
} from "../transcript-live-tools";
import { TurnEntry } from "./turn-entry";
import { groupItemsForDisplay } from "./turns";
import type { TranscriptPaneProps } from "./types";

export type { TranscriptPaneProps } from "./types";

export function TranscriptPane(props: TranscriptPaneProps) {
	const [liveTools, setLiveTools] = createSignal<LiveToolExecutionMap>({});

	createEffect(() => {
		setLiveTools((prev) => reconcileLiveTools(prev, props.items));
	});

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
	});

	return (
		<scrollbox
			flexGrow={1}
			height="100%"
			scrollY
			stickyStart="bottom"
			stickyScroll
			padding={1}
			style={{
				scrollbarOptions: {
					trackOptions: {
						foregroundColor: theme.scrollbarFg,
						backgroundColor: theme.scrollbarBg,
					},
				},
			}}
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
					<For each={groupItemsForDisplay(props.items)}>
						{(displayItem, index) => {
							const turnId = () =>
								displayItem.kind === "single"
									? displayItem.item.turnId
									: displayItem.turnId;
							const itemKind = () =>
								displayItem.kind === "single"
									? displayItem.item.kind
									: "tool-group";
							// Extra spacing before user messages for visual separation
							const spacerHeight = () => {
								if (index() === 0) return 0;
								if (itemKind() === "user") return 2;
								return 1;
							};
							return (
								<>
									<Show when={spacerHeight() > 0}>
										<box height={spacerHeight()} />
									</Show>
									<TurnEntry
										displayItem={displayItem}
										liveTools={liveTools()[turnId()] ?? {}}
										showToast={props.showToast}
										zenMode={props.zenMode}
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
