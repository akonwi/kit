import type { Renderable } from "@opentui/core";
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
 */
export type TurnActivitySection =
	| {
			kind: "assistant";
			message: AssistantMessage;
			toolResults: Map<string, ToolResultMessage>;
			aborted?: boolean;
	  }
	| {
			kind: "bash";
			message: BashExecutionMessage;
	  }
	| {
			kind: "handoff-summary";
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
				message: item.message,
				toolResults: item.toolResults,
				aborted: item.aborted,
			});
		} else if (item.kind === "bash") {
			sections.push({ kind: "bash", message: item.message });
		} else if (item.kind === "handoff-summary") {
			sections.push({
				kind: "handoff-summary",
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
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);

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
		target: rootTarget,
		targetMode: "focus-within",
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
			rootRef={setRootTarget}
			rootFocusable
			rootFocused={props.active !== false}
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
							<For each={sections()}>
								{(section) => (
									<Switch>
										<Match when={section.kind === "assistant" && section}>
											{(s) => (
												<FlatAssistantEntry
													msg={s().message}
													toolResults={s().toolResults}
													liveTools={turnLiveTools()}
													aborted={s().aborted}
													autoExpand
													fullArgs
													noTruncate
													enrichOutput
												/>
											)}
										</Match>
										<Match when={section.kind === "bash" && section}>
											{(s) => (
												<DialogCard>
													<BashEntry msg={s().message} noTruncate />
												</DialogCard>
											)}
										</Match>
										<Match when={section.kind === "handoff-summary" && section}>
											{(s) => (
												<DialogCard>
													<HandoffSummaryEntry
														msg={s().message}
														aborted={s().aborted}
														autoExpand
													/>
												</DialogCard>
											)}
										</Match>
									</Switch>
								)}
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
