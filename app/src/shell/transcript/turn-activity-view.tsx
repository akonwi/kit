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
import type { AssistantMessage, ToolResultMessage } from "../../runtime/agent";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { theme } from "../theme";
import {
	type LiveToolExecutionMap,
	type LiveToolsForTurn,
	reconcileLiveTools,
	upsertLiveTool,
} from "../transcript-live-tools";
import { FlatAssistantEntry } from "./assistant-entry";
import { BashEntry } from "./bash-entry";
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
 * Heterogeneous section types shown in the activity view.
 *
 * Each section carries the stable id of its originating TranscriptItem so
 * the For loop can key on it and keep child components mounted across
 * re-renders.
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
 * Identifies the live data the view should render.
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
	inProgressTurnId: string | null,
): { sections: TurnActivitySection[]; turnId: string } {
	if (source.kind === "single-item") {
		const item = items.find((i) => i.id === source.itemId);
		if (!item) return { sections: [], turnId: "" };
		return {
			sections: itemsToSections([item]),
			turnId: item.turnId,
		};
	}
	// Pass the in-progress turn id through so the source turn folds into a
	// turn-work display item even when grouping wouldn't normally produce
	// one (e.g. ≤ 1 assistant item so far, or bash/handoff-only activity).
	// Without this, an open sidebar can show "No activity to display" until
	// enough messages accumulate or the turn completes.
	const displayItems = groupItemsForDisplay(items, inProgressTurnId);
	for (const d of displayItems) {
		if (d.kind === "turn-work" && d.turnId === source.turnId) {
			return { sections: itemsToSections(d.items), turnId: d.turnId };
		}
	}
	return { sections: [], turnId: source.turnId };
}

export function countToolCalls(sections: TurnActivitySection[]): number {
	let n = 0;
	for (const section of sections) {
		if (section.kind === "assistant") {
			n += extractAssistantParts(section.message).toolCalls.length;
		}
	}
	return n;
}

export type TurnActivityModel = {
	sections: () => TurnActivitySection[];
	sectionOrder: () => string[];
	sectionsById: () => Map<string, TurnActivitySection>;
	turnLiveTools: () => LiveToolsForTurn;
	toolCallCount: () => number;
	stepCount: () => number;
	/**
	 * Snapshot taken at model creation: was the source's turn streaming?
	 * Drives stickyStart/stickyScroll on the consuming scrollbox so live
	 * turns auto-follow new content while completed turns open at the top.
	 * Non-reactive on purpose to avoid surprise scroll jumps if the turn
	 * transitions to/from streaming while the view is open.
	 */
	initiallyLive: boolean;
};

/**
 * Builds the reactive activity model from the runtime, subscribing to all
 * relevant events so the view stays live while open. Wires its own
 * onCleanup so callers don't need to thread teardown.
 *
 * `onSessionChange` is invoked when the active session swaps underneath
 * the view; the consumer is responsible for closing/dismissing.
 *
 * Note: `source` is captured by value. The reactive model is built once at
 * creation and does not observe later changes to the parameter. Callers
 * that need to swap sources (e.g. the sidebar) must re-mount the
 * consuming component (e.g. with `<Show keyed>`) so this function re-runs
 * with the new source.
 */
export function createTurnActivityModel(
	runtime: AgentRuntime,
	source: ActivitySource,
	onSessionChange: () => void,
): TurnActivityModel {
	// Snapshot whether the source's turn was streaming at the moment the
	// view opened. See `initiallyLive` doc on TurnActivityModel.
	const initiallyLive = ((): boolean => {
		if (!runtime.getStatus().isStreaming) return false;
		const turns = runtime.getTurns();
		const lastTurn = turns[turns.length - 1];
		if (!lastTurn) return false;
		if (source.kind === "turn-intermediate") {
			return source.turnId === lastTurn.id;
		}
		// single-item: find the originating item's turn
		const items = flattenTurnsToTranscriptItems(turns);
		const item = items.find((i) => i.id === source.itemId);
		return item?.turnId === lastTurn.id;
	})();
	const [tick, setTick] = createSignal(0);
	const bumpTick = () => setTick((t) => t + 1);

	const [liveTools, setLiveTools] = createSignal<LiveToolExecutionMap>({});

	// Mirror TranscriptPane's in-progress turn tracking so the source
	// turn folds into a single turn-work item even before its first
	// assistant message ends.
	const [inProgressTurnId, setInProgressTurnId] = createSignal<string | null>(
		(() => {
			if (!runtime.getStatus().isStreaming) return null;
			return runtime.getTurns().at(-1)?.id ?? null;
		})(),
	);

	const sectionsAndTurn = createMemo(() => {
		tick();
		const items = flattenTurnsToTranscriptItems(runtime.getTurns());
		return buildSectionsForSource(items, source, inProgressTurnId());
	});

	const sections = () => sectionsAndTurn().sections;
	const activeTurnId = () => sectionsAndTurn().turnId;

	const sectionOrder = createMemo(() => sections().map((s) => s.id));
	const sectionsById = createMemo(() => {
		const map = new Map<string, TurnActivitySection>();
		for (const s of sections()) map.set(s.id, s);
		return map;
	});

	const turnLiveTools = (): LiveToolsForTurn =>
		liveTools()[activeTurnId()] ?? {};

	// Reconcile live tools so completed entries don't linger after the result
	// lands.
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

	const unsubs: Array<() => void> = [];

	const refreshEvents = [
		"agent.message.ended",
		"agent.turn.completed",
		"agent.tool.ended",
		"bash.command.started",
		"bash.command.completed",
	] as const;
	for (const evt of refreshEvents) {
		unsubs.push(runtime.subscribe(evt, bumpTick));
	}
	unsubs.push(
		runtime.subscribe({ prefix: "session.compaction.completed" }, bumpTick),
	);

	unsubs.push(
		runtime.subscribe("agent.tool.started", (event) => {
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
		runtime.subscribe("agent.tool.updated", (event) => {
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
		runtime.subscribe("agent.tool.ended", (event) => {
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

	unsubs.push(
		runtime.subscribe("agent.turn.started", (event) => {
			setInProgressTurnId(event.turn.id);
		}),
	);
	unsubs.push(
		runtime.subscribe("agent.turn.completed", () => {
			setInProgressTurnId(null);
		}),
	);

	unsubs.push(
		runtime.subscribe("session.active.changed", () => {
			setInProgressTurnId(null);
			onSessionChange();
		}),
	);

	onCleanup(() => {
		for (const u of unsubs) u();
	});

	const toolCallCount = () => countToolCalls(sections());
	const stepCount = () => sections().length;

	return {
		sections,
		sectionOrder,
		sectionsById,
		turnLiveTools,
		toolCallCount,
		stepCount,
		initiallyLive,
	};
}

/**
 * Inner section list rendered identically by the modal dialog and the
 * sidebar. Wrapped in a scrollbox by the caller so each chrome can apply
 * its own padding/border around the scroll region.
 */
export function TurnActivitySectionList(props: { model: TurnActivityModel }) {
	return (
		<Show
			when={props.model.sections().length > 0}
			fallback={
				<box flexGrow={1} justifyContent="center" alignItems="center">
					<text fg={theme.textMuted}>Nothing to show here yet</text>
				</box>
			}
		>
			<box flexDirection="column" gap={0} width="100%">
				<For each={props.model.sectionOrder()}>
					{(id) => {
						// Per-kind narrowed accessors so Solid's Match keeps
						// reactive type narrowing through the union.
						const section = createMemo(() =>
							props.model.sectionsById().get(id),
						);
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
											liveTools={props.model.turnLiveTools()}
											aborted={a().aborted}
											fullArgs
											noTruncate
											enrichOutput
										/>
									)}
								</Match>
								<Match when={asBash()}>
									{(b) => <BashEntry msg={b().message} noTruncate plain />}
								</Match>
								<Match when={asHandoff()}>
									{(h) => (
										<HandoffSummaryEntry
											msg={h().message}
											aborted={h().aborted}
										/>
									)}
								</Match>
							</Switch>
						);
					}}
				</For>
			</box>
		</Show>
	);
}
