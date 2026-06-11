import {
	createEffect,
	createMemo,
	createSignal,
	For,
	Match,
	onCleanup,
	Show,
	Switch,
} from "solid-js";
import type {
	OverlayComponentProps,
	OverlaySurfaceProps,
} from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { AssistantMessage, ToolResultMessage } from "../../runtime/agent";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { Dialog } from "../Dialog";
import { KeymapHintBar } from "../KeymapHintBar";
import { theme } from "../theme";
import {
	type LiveToolExecutionMap,
	type LiveToolsForTurn,
	reconcileLiveTools,
	upsertLiveTool,
} from "../transcript-live-tools";
import { FlatAssistantEntry } from "./assistant-entry";
import { BashEntry } from "./bash-entry";
import { DialogCard } from "./dialog-card";
import { HandoffSummaryEntry } from "./handoff-summary-entry";
import type {
	BashExecutionMessage,
	HandoffSummaryMessage,
	TranscriptItem,
} from "./turns";
import {
	extractAssistantParts,
	flattenTurnsToTranscriptItems,
	groupItemsForDisplay,
} from "./turns";

/**
 * Heterogeneous section types shown in the activity dialog.
 *
 * Each section carries the stable id of its originating TranscriptItem so
 * the For loop in the dialog can key on it and keep child components
 * mounted across re-renders. Without a stable key the For would remount on
 * every tick (since `itemsToSections` rebuilds object refs), wiping out
 * local UI state like collapsed/expanded tool calls.
 */
export type TurnActivitySection =
	| {
			kind: "assistant";
			id: string;
			message: AssistantMessage;
			toolResults: Map<string, ToolResultMessage>;
			aborted?: boolean;
	  }
	| {
			kind: "bash";
			id: string;
			message: BashExecutionMessage;
	  }
	| {
			kind: "handoff-summary";
			id: string;
			message: HandoffSummaryMessage;
			aborted?: boolean;
	  };

/**
 * Identifies the live data the dialog should render.
 *
 * - `single-item` shows one assistant message (e.g. invoked from a single-
 *   message ToolDrawer chip).
 * - `turn-intermediate` shows the intermediate items of a turn (everything
 *   except the user message and the final prose message), matching the
 *   TurnWorkDrawer chip.
 */
export type ActivitySource =
	| { kind: "single-item"; itemId: string }
	| { kind: "turn-intermediate"; turnId: string };

export type TurnActivityDialogProps = OverlayComponentProps<unknown> & {
	runtime: AgentRuntime;
	source: ActivitySource;
	surfaceProps?: OverlaySurfaceProps;
};

function itemsToSections(items: TranscriptItem[]): TurnActivitySection[] {
	const sections: TurnActivitySection[] = [];
	for (const item of items) {
		if (item.kind === "assistant") {
			sections.push({
				kind: "assistant",
				id: item.id,
				message: item.message,
				toolResults: item.toolResults,
				aborted: item.aborted,
			});
		} else if (item.kind === "bash") {
			sections.push({ kind: "bash", id: item.id, message: item.message });
		} else if (item.kind === "handoff-summary") {
			sections.push({
				kind: "handoff-summary",
				id: item.id,
				message: item.message,
				aborted: item.aborted,
			});
		}
	}
	return sections;
}

function buildSectionsForSource(
	items: TranscriptItem[],
	source: ActivitySource,
): { sections: TurnActivitySection[]; turnId: string } {
	if (source.kind === "single-item") {
		const item = items.find((i) => i.id === source.itemId);
		if (!item) return { sections: [], turnId: "" };
		return {
			sections: itemsToSections([item]),
			turnId: item.turnId,
		};
	}
	const displayItems = groupItemsForDisplay(items);
	for (const d of displayItems) {
		if (d.kind === "turn-work" && d.turnId === source.turnId) {
			return { sections: itemsToSections(d.items), turnId: d.turnId };
		}
	}
	return { sections: [], turnId: source.turnId };
}

function countToolCalls(sections: TurnActivitySection[]): number {
	let n = 0;
	for (const section of sections) {
		if (section.kind === "assistant") {
			n += extractAssistantParts(section.message).toolCalls.length;
		}
	}
	return n;
}

/**
 * Modal that presents the rich activity of a turn (or part of a turn) —
 * prose, tool calls with auto-expanded output, bash, handoffs.
 *
 * Reads live data directly from the runtime so updates flowing in while
 * the dialog is open are reflected without requiring close/reopen.
 */
export function TurnActivityDialog(props: TurnActivityDialogProps) {
	// Sections are derived from a `tick` signal that we increment whenever a
	// runtime event arrives that could change the activity content.
	const [tick, setTick] = createSignal(0);
	const bumpTick = () => setTick((t) => t + 1);

	// Live tool state for the relevant turn. Mirrors the pane's live-tools
	// reconciliation but scoped to this dialog so updates flow even if the
	// originating drawer is unmounted.
	const [liveTools, setLiveTools] = createSignal<LiveToolExecutionMap>({});

	const sectionsAndTurn = createMemo(() => {
		tick();
		const items = flattenTurnsToTranscriptItems(props.runtime.getTurns());
		return buildSectionsForSource(items, props.source);
	});

	const sections = () => sectionsAndTurn().sections;
	const activeTurnId = () => sectionsAndTurn().turnId;

	// Stable id list and id->section lookup. The For below keys on string ids
	// so existing rows keep their identity (and their collapsed/expanded state)
	// as new sections stream in. Children read the latest section reactively
	// through `sectionsById()`.
	const sectionOrder = createMemo(() => sections().map((s) => s.id));
	const sectionsById = createMemo(() => {
		const map = new Map<string, TurnActivitySection>();
		for (const s of sections()) map.set(s.id, s);
		return map;
	});

	const turnLiveTools = (): LiveToolsForTurn =>
		liveTools()[activeTurnId()] ?? {};

	// Keep live tool state in sync with the latest sections so completed tool
	// entries don't linger after the result lands.
	createEffect(() => {
		const reconcileItems: TranscriptItem[] = [];
		for (const section of sections()) {
			if (section.kind === "assistant") {
				reconcileItems.push({
					kind: "assistant",
					id: "live-tools-reconcile",
					turnId: activeTurnId(),
					message: section.message,
					toolResults: section.toolResults,
					aborted: section.aborted ?? false,
				});
			}
		}
		setLiveTools((prev) => reconcileLiveTools(prev, reconcileItems));
	});

	// Subscribe to runtime events.
	const unsubs: Array<() => void> = [];

	const refreshEvents = [
		"agent.message.ended",
		"agent.turn.completed",
		"agent.tool.ended",
		"bash.command.started",
		"bash.command.completed",
	] as const;
	for (const evt of refreshEvents) {
		unsubs.push(props.runtime.subscribe(evt, bumpTick));
	}
	unsubs.push(
		props.runtime.subscribe(
			{ prefix: "session.compaction.completed" },
			bumpTick,
		),
	);

	unsubs.push(
		props.runtime.subscribe("agent.tool.started", (event) => {
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
		}),
	);
	unsubs.push(
		props.runtime.subscribe("agent.tool.updated", (event) => {
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
		}),
	);
	unsubs.push(
		props.runtime.subscribe("agent.tool.ended", (event) => {
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
		}),
	);

	// Close the dialog if the session changes underneath us — the source ids
	// no longer make sense in the new session.
	unsubs.push(
		props.runtime.subscribe("session.active.changed", () => {
			props.done(undefined);
		}),
	);

	onCleanup(() => {
		for (const u of unsubs) u();
	});

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active !== false,
		commands: {
			"turn-activity.close": () => props.done(undefined),
		},
	}));

	const toolCallCount = () => countToolCalls(sections());
	const stepCount = () => sections().length;

	return (
		<Dialog.Root
			width="90%"
			maxWidth={160}
			height="85%"
			surfaceProps={props.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title>Turn activity</Dialog.Title>
				<Dialog.Meta>
					{toolCallCount()} tool call{toolCallCount() === 1 ? "" : "s"}
					{" · "}
					{stepCount()} step{stepCount() === 1 ? "" : "s"}
				</Dialog.Meta>
			</Dialog.Header>

			<Dialog.Body>
				<scrollbox
					flexGrow={1}
					scrollY
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
						when={sections().length > 0}
						fallback={
							<box flexGrow={1} justifyContent="center" alignItems="center">
								<text fg={theme.textMuted}>No activity to display.</text>
							</box>
						}
					>
						<box flexDirection="column" gap={1} width="100%">
							<For each={sectionOrder()}>
								{(id) => {
									// Per-kind narrowed accessors so Solid's Match/Show keep
									// reactive type narrowing through the union.
									const section = createMemo(() => sectionsById().get(id));
									const asAssistant = createMemo(() => {
										const s = section();
										return s && s.kind === "assistant" ? s : undefined;
									});
									const asBash = createMemo(() => {
										const s = section();
										return s && s.kind === "bash" ? s : undefined;
									});
									const asHandoff = createMemo(() => {
										const s = section();
										return s && s.kind === "handoff-summary" ? s : undefined;
									});
									return (
										<Switch>
											<Match when={asAssistant()}>
												{(a) => (
													<FlatAssistantEntry
														msg={a().message}
														toolResults={a().toolResults}
														liveTools={turnLiveTools()}
														aborted={a().aborted}
														autoExpand
														fullArgs
														noTruncate
														enrichOutput
													/>
												)}
											</Match>
											<Match when={asBash()}>
												{(b) => (
													<DialogCard>
														<BashEntry msg={b().message} noTruncate />
													</DialogCard>
												)}
											</Match>
											<Match when={asHandoff()}>
												{(h) => (
													<DialogCard>
														<HandoffSummaryEntry
															msg={h().message}
															aborted={h().aborted}
															autoExpand
														/>
													</DialogCard>
												)}
											</Match>
										</Switch>
									);
								}}
							</For>
						</box>
					</Show>
				</scrollbox>
			</Dialog.Body>

			<Dialog.Footer>
				<KeymapHintBar borderless group="turn-activity" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
