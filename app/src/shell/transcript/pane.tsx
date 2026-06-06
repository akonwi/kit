import "../../runtime/custom-messages";
import { createEffect, createSignal, Index, onCleanup, Show } from "solid-js";
import { HEAVY_LINE, HORIZONTAL_LINE } from "../glyphs";
import { theme } from "../theme";
import {
	type LiveToolExecutionMap,
	reconcileLiveTools,
	upsertLiveTool,
} from "../transcript-live-tools";
import { TurnEntry } from "./turn-entry";
import type { TranscriptPaneProps } from "./types";

export type { TranscriptPaneProps } from "./types";

function TurnDivider() {
	const [width, setWidth] = createSignal(0);
	let ref: { width: number; height: number } | undefined;
	return (
		<box
			ref={(value) => {
				ref = value as typeof ref;
				if (ref) setWidth(ref.width);
			}}
			onSizeChange={() => {
				if (ref) setWidth(ref.width);
			}}
			width="100%"
			paddingY={1}
			justifyContent="center"
		>
			<text fg={theme.borderDefault}>
				{HORIZONTAL_LINE.repeat(Math.max(0, width() - 2))}
			</text>
		</box>
	);
}

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
					<Index each={props.items}>
						{(item, index) => {
							const prevItem = () =>
								index > 0 ? props.items[index - 1] : undefined;
							const showDivider = () => {
								const prev = prevItem();
								return prev !== undefined && prev.turnId !== item().turnId;
							};
							return (
								<>
									<Show when={showDivider()}>
										<TurnDivider />
									</Show>
									<Show when={index > 0 && !showDivider()}>
										<box height={1} />
									</Show>
									<TurnEntry
										item={item()}
										liveTools={liveTools()[item().turnId] ?? {}}
										showToast={props.showToast}
										zenMode={props.zenMode}
									/>
								</>
							);
						}}
					</Index>
				</box>
			</Show>
		</scrollbox>
	);
}
