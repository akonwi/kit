import type { Renderable } from "@opentui/core";
import { useTerminalDimensions } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	For,
	onCleanup,
	Show,
} from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { ToolResultMessage } from "../../runtime/agent";
import type { SessionEntry, Turn } from "../../session";
import { Dialog } from "../../shell/Dialog";
import {
	CHEVRON_RIGHT,
	CIRCLE_EMPTY,
	CIRCLE_FILLED,
	CIRCLE_SLASH,
	CROSS,
	HEAVY_LINE,
	MIDDLE_DOT,
} from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { scrollbarStyle, theme } from "../../shell/theme";
import { FlatAssistantEntry } from "../../shell/transcript/assistant-entry";
import { UserEntry } from "../../shell/transcript/user-entry";
import type { LiveToolsForTurn } from "../../shell/transcript-live-tools";
import type { SubagentDefinition } from "./discovery";
import {
	type ActiveSubagentConversationState,
	type ActiveSubagentStatus,
	buildSubagentTranscriptTurns,
} from "./state";

type SubagentDisplayStatus = ActiveSubagentStatus | "inactive";
type ViewMode = "list" | "transcript" | "confirmDismiss";

type SubagentListItem = {
	name: string;
	description: string;
	model?: string;
	source?: SubagentDefinition["source"];
	pluginName?: string;
	status: SubagentDisplayStatus;
	lastActivityAt?: string;
	conversation?: ActiveSubagentConversationState;
};

export type SubagentsStatusModalProps = {
	surfaceProps?: OverlaySurfaceProps;
	getAgents: () => SubagentDefinition[];
	getActiveConversations: () => ActiveSubagentConversationState[];
	readConversationEntries: (conversationId: string) => Promise<SessionEntry[]>;
	subscribeToChanges: (listener: () => void) => () => void;
	dismissConversation: (agentName: string) => Promise<boolean>;
	active?: boolean;
	onClose: () => void;
};

const SUBAGENT_LIST_WIDTH = 36;

const STATUS_RANK: Record<SubagentDisplayStatus, number> = {
	running: 0,
	failed: 1,
	aborted: 2,
	idle: 3,
	inactive: 4,
};

function statusLabel(status: SubagentDisplayStatus): string {
	if (status === "idle") return "completed";
	if (status === "inactive") return "available";
	return status;
}

function statusIndicator(status: SubagentDisplayStatus): {
	glyph: string;
	color: string;
} {
	switch (status) {
		case "running":
			return { glyph: CIRCLE_FILLED, color: theme.subagentText };
		case "failed":
			return { glyph: CROSS, color: theme.errorText };
		case "aborted":
			return { glyph: CIRCLE_SLASH, color: theme.warningText };
		case "idle":
			return { glyph: CIRCLE_EMPTY, color: theme.textSecondary };
		case "inactive":
			return { glyph: CIRCLE_EMPTY, color: theme.textMuted };
	}
}

function sourceLabel(item: Pick<SubagentListItem, "source" | "pluginName">) {
	switch (item.source) {
		case "kit-user":
			return "user";
		case "kit-project":
			return "project";
		case "plugin":
			return item.pluginName ? `plugin:${item.pluginName}` : "plugin";
		case undefined:
			return "active";
	}
}

function relativeTime(iso: string | undefined): string {
	if (!iso) return "";
	const timestamp = new Date(iso).getTime();
	if (Number.isNaN(timestamp)) return "";
	const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
	if (seconds < 60) return "just now";
	if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
	if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
	return `${Math.floor(seconds / 86400)}d ago`;
}

function mergeItems(
	agents: SubagentDefinition[],
	activeConversations: ActiveSubagentConversationState[],
): SubagentListItem[] {
	const activeByName = new Map(
		activeConversations.map((conversation) => [
			conversation.agentName,
			conversation,
		]),
	);
	const items = agents.map<SubagentListItem>((agent) => {
		const conversation = activeByName.get(agent.name);
		return {
			name: agent.name,
			description: agent.description,
			model: conversation?.model ?? agent.model,
			source: agent.source,
			pluginName: agent.pluginName,
			status: conversation?.status ?? "inactive",
			lastActivityAt: conversation?.lastActivityAt,
			conversation,
		};
	});
	const agentNames = new Set(agents.map((agent) => agent.name));
	for (const conversation of activeConversations) {
		if (agentNames.has(conversation.agentName)) continue;
		items.push({
			name: conversation.agentName,
			description:
				conversation.description ?? "Previously active sub-agent conversation",
			model: conversation.model,
			status: conversation.status,
			lastActivityAt: conversation.lastActivityAt,
			conversation,
		});
	}
	return items.sort((a, b) => {
		const rank = STATUS_RANK[a.status] - STATUS_RANK[b.status];
		return rank !== 0 ? rank : a.name.localeCompare(b.name);
	});
}

function toolResultsForTurn(turn: Turn): Map<string, ToolResultMessage> {
	const results = new Map<string, ToolResultMessage>();
	for (const message of turn.messages) {
		if (message.role === "toolResult") results.set(message.toolCallId, message);
	}
	return results;
}

function liveToolsFor(
	conversation: ActiveSubagentConversationState,
): LiveToolsForTurn {
	return Object.fromEntries(
		Object.entries(conversation.liveTools ?? {}).map(([id, tool]) => [
			id,
			{ ...tool, turnId: "live" },
		]),
	);
}

function TranscriptView(props: {
	conversation: ActiveSubagentConversationState;
	entries: SessionEntry[];
	setScrollRef: (
		ref: Renderable & {
			scrollBy: (opts: { x: number; y: number }) => void;
			scrollTo: (opts: { x?: number; y?: number } | number) => void;
		},
	) => void;
}) {
	const turns = createMemo(() =>
		buildSubagentTranscriptTurns(
			props.entries,
			props.conversation.subagentConversationId,
		),
	);
	return (
		<scrollbox
			ref={(element) =>
				props.setScrollRef(element as Parameters<typeof props.setScrollRef>[0])
			}
			flexGrow={1}
			scrollY
			stickyStart="bottom"
			stickyScroll
			style={scrollbarStyle()}
		>
			<box flexDirection="column" gap={1} paddingX={1} width="100%">
				<For each={turns()}>
					{(turn) => {
						const results = toolResultsForTurn(turn);
						return (
							<For each={turn.messages}>
								{(message, index) => {
									if (message.role === "user") {
										return (
											<UserEntry
												msg={message}
												sourceId={`${turn.id}:${index()}`}
												showToast={() => {}}
												openReviewAttachment={() => {}}
											/>
										);
									}
									if (message.role === "assistant") {
										return (
											<FlatAssistantEntry
												msg={message}
												toolResults={results}
												liveTools={{}}
												fullArgs
											/>
										);
									}
									return null;
								}}
							</For>
						);
					}}
				</For>
				<Show when={turns().length === 0 && !props.conversation.liveMessage}>
					<text fg={theme.textMuted}>Waiting for sub-agent activity…</text>
				</Show>
				<Show when={props.conversation.liveMessage}>
					{(message) => (
						<FlatAssistantEntry
							msg={message()}
							toolResults={new Map()}
							liveTools={liveToolsFor(props.conversation)}
							fullArgs
						/>
					)}
				</Show>
				<Show when={props.conversation.status === "failed"}>
					<text fg={theme.errorText}>
						{CROSS} {props.conversation.failureMessage ?? "Sub-agent failed"}
					</text>
				</Show>
				<Show when={props.conversation.status === "aborted"}>
					<text fg={theme.warningText}>
						{CIRCLE_SLASH}{" "}
						{props.conversation.abortReason ?? "Sub-agent aborted"}
					</text>
				</Show>
			</box>
		</scrollbox>
	);
}

function EmptyState() {
	return (
		<box
			flexGrow={1}
			flexDirection="column"
			justifyContent="center"
			alignItems="center"
			gap={1}
		>
			<text fg={theme.textPrimary}>k i t</text>
			<text fg={theme.borderAccent}>{HEAVY_LINE.repeat(11)}</text>
			<text fg={theme.textSecondary}>No sub-agents available</text>
			<text fg={theme.textPlaceholder}>
				Add .md files to ~/.kit/agents/ or use plugins
			</text>
		</box>
	);
}

export function SubagentsStatusModal(props: SubagentsStatusModalProps) {
	const terminalDimensions = useTerminalDimensions();
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);
	const [transcriptTarget, setTranscriptTarget] =
		createSignal<Renderable | null>(null);
	const [selectedName, setSelectedName] = createSignal("");
	const [mode, setMode] = createSignal<ViewMode>("list");
	const [dismissReturnMode, setDismissReturnMode] = createSignal<
		"list" | "transcript"
	>("list");
	const [dismissing, setDismissing] = createSignal(false);
	const [dismissError, setDismissError] = createSignal<string | null>(null);
	const [revision, setRevision] = createSignal(0);
	let listScrollRef:
		| { scrollTo: (opts: { x?: number; y?: number } | number) => void }
		| undefined;
	let transcriptScrollRef:
		| {
				scrollBy: (opts: { x: number; y: number }) => void;
				scrollTo: (opts: { x?: number; y?: number } | number) => void;
		  }
		| undefined;
	const [clock, setClock] = createSignal(0);
	const timer = setInterval(() => setClock((value) => value + 1), 30_000);
	const unsubscribe = props.subscribeToChanges(() =>
		setRevision((value) => value + 1),
	);
	onCleanup(() => {
		clearInterval(timer);
		setTranscriptTarget(null);
		unsubscribe();
	});

	const items = createMemo(() => {
		revision();
		return mergeItems(props.getAgents(), props.getActiveConversations());
	});
	const selectedItem = createMemo(
		() => items().find((item) => item.name === selectedName()) ?? null,
	);
	const selectedIndex = createMemo(() =>
		items().findIndex((item) => item.name === selectedName()),
	);
	const selectedConversation = createMemo(
		() => selectedItem()?.conversation ?? null,
		undefined,
		{ equals: false },
	);
	const wide = createMemo(() => terminalDimensions().width >= 90);
	const detailConversation = createMemo(
		() => (mode() === "transcript" ? selectedConversation() : null),
		undefined,
		{ equals: false },
	);
	const transcriptKey = createMemo(
		() => {
			const conversation = selectedConversation();
			return conversation
				? ([
						conversation.subagentConversationId,
						conversation.transcriptRevision ?? 0,
					] as const)
				: undefined;
		},
		undefined,
		{
			equals: (previous, next) =>
				previous?.[0] === next?.[0] && previous?.[1] === next?.[1],
		},
	);
	const [entries] = createResource(transcriptKey, async ([id]) =>
		props.readConversationEntries(id),
	);
	const runningCount = createMemo(
		() => items().filter((item) => item.status === "running").length,
	);

	createEffect(() => {
		const allItems = items();
		if (allItems.length === 0) {
			setSelectedName("");
			return;
		}
		if (!allItems.some((item) => item.name === selectedName())) {
			setSelectedName(allItems[0]?.name ?? "");
		}
		if (!selectedConversation() && mode() !== "list") setMode("list");
	});

	createEffect(() => {
		const index = selectedIndex();
		if (index >= 0)
			listScrollRef?.scrollTo({ x: 0, y: Math.max(0, index * 3 - 3) });
	});

	function currentRelativeTime(iso: string | undefined): string {
		clock();
		return relativeTime(iso);
	}

	function moveSelection(delta: number) {
		const allItems = items();
		if (allItems.length === 0) return;
		const index = selectedIndex();
		const nextIndex = Math.max(
			0,
			Math.min(allItems.length - 1, (index < 0 ? 0 : index) + delta),
		);
		setSelectedName(allItems[nextIndex]?.name ?? "");
	}

	function openTranscript() {
		if (selectedConversation()) setMode("transcript");
	}

	function beginDismiss() {
		if (!selectedConversation()) return;
		setDismissReturnMode(mode() === "transcript" ? "transcript" : "list");
		setDismissError(null);
		setMode("confirmDismiss");
	}

	async function confirmDismiss() {
		const target = selectedConversation();
		if (!target || dismissing()) return;
		setDismissing(true);
		setDismissError(null);
		try {
			await props.dismissConversation(target.agentName);
			setMode("list");
			setRevision((value) => value + 1);
		} catch (error) {
			setDismissError(error instanceof Error ? error.message : String(error));
		} finally {
			setDismissing(false);
		}
	}

	useKeymapLayer(() => ({
		scope: "modal",
		target: rootTarget,
		targetMode: "focus-within",
		when: () => props.active !== false && mode() === "list",
		commands: {
			"subagents.close": props.onClose,
			"subagents.move-up": () => moveSelection(-1),
			"subagents.move-down": () => moveSelection(1),
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		target: rootTarget,
		targetMode: "focus-within",
		when: () =>
			props.active !== false &&
			mode() === "list" &&
			Boolean(selectedConversation()),
		commands: {
			"subagents.open": openTranscript,
			"subagents.dismiss": beginDismiss,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		target: transcriptTarget,
		when: () => props.active !== false && mode() === "transcript",
		commands: {
			"subagents.back": () => {
				setTranscriptTarget(null);
				setMode("list");
			},
			"subagents.scroll-up": () => {
				transcriptScrollRef?.scrollBy({ x: 0, y: -1 });
			},
			"subagents.scroll-down": () => {
				transcriptScrollRef?.scrollBy({ x: 0, y: 1 });
			},
			"subagents.scroll-top": () => {
				transcriptScrollRef?.scrollTo({ x: 0, y: 0 });
			},
			"subagents.scroll-bottom": () => {
				transcriptScrollRef?.scrollTo({ x: 0, y: Number.MAX_SAFE_INTEGER });
			},
			"subagents.dismiss-transcript": beginDismiss,
		},
	}));

	useKeymapLayer(() => ({
		scope: "modal",
		target: rootTarget,
		targetMode: "focus-within",
		when: () => props.active !== false && mode() === "confirmDismiss",
		commands: {
			"subagents.confirm-dismiss": () => void confirmDismiss(),
			"subagents.cancel-dismiss": () => {
				if (!dismissing()) setMode(dismissReturnMode());
			},
		},
	}));

	return (
		<Dialog.Root
			width="85%"
			height="70%"
			maxWidth={140}
			minWidth={44}
			paddingBottom={0}
			surfaceProps={props.surfaceProps}
			rootRef={setRootTarget}
			rootFocusable
			rootFocused={
				props.active !== false &&
				(mode() === "list" ||
					mode() === "confirmDismiss" ||
					items().length === 0)
			}
		>
			<Dialog.Header>
				<Dialog.Title>
					{mode() === "transcript" && !wide()
						? (selectedItem()?.name ?? "Sub-agent")
						: "Sub-agents"}
				</Dialog.Title>
				<Dialog.Meta>
					{runningCount() > 0
						? `${runningCount()} running ${MIDDLE_DOT} ${items().filter((item) => item.conversation).length} conversations`
						: `${items().filter((item) => item.conversation).length} conversations`}
				</Dialog.Meta>
			</Dialog.Header>

			<Dialog.Body>
				<Show when={items().length > 0} fallback={<EmptyState />}>
					<box flexGrow={1} flexDirection="row" width="100%">
						<Show when={wide() || mode() === "list"}>
							<scrollbox
								ref={(element) => {
									listScrollRef = element as typeof listScrollRef;
								}}
								width={wide() ? SUBAGENT_LIST_WIDTH : "100%"}
								flexShrink={0}
								scrollY
								style={scrollbarStyle()}
							>
								<box flexDirection="column" gap={0} width="100%">
									<For each={items()}>
										{(item, index) => {
											const indicator = () => statusIndicator(item.status);
											const selected = () => index() === selectedIndex();
											return (
												<>
													<Show
														when={
															item.status === "inactive" &&
															items()[index() - 1]?.status !== "inactive"
														}
													>
														<text fg={theme.textMuted} paddingX={1}>
															Available
														</text>
													</Show>
													<box
														flexDirection="column"
														paddingX={1}
														backgroundColor={
															selected()
																? theme.pickerFocusedBg
																: theme.bgTransparent
														}
														onMouseUp={() => {
															setSelectedName(item.name);
															setMode(
																item.conversation && !wide()
																	? "transcript"
																	: "list",
															);
														}}
													>
														<box
															flexDirection="row"
															justifyContent="space-between"
															gap={1}
														>
															<text fg={indicator().color} truncate>
																{indicator().glyph} {item.name}
															</text>
															<text fg={indicator().color}>
																{statusLabel(item.status)}{" "}
																{item.conversation ? CHEVRON_RIGHT : ""}
															</text>
														</box>
														<text fg={theme.textMuted} truncate>
															{item.description}
														</text>
														<text fg={theme.textPlaceholder} truncate>
															{item.model ? `${item.model} ${MIDDLE_DOT} ` : ""}
															{sourceLabel(item)}
															{item.lastActivityAt
																? ` ${MIDDLE_DOT} ${currentRelativeTime(item.lastActivityAt)}`
																: ""}
														</text>
													</box>
												</>
											);
										}}
									</For>
								</box>
							</scrollbox>
						</Show>
						<Show
							when={detailConversation()}
							fallback={
								<Show when={wide()}>
									<box
										flexGrow={1}
										border={["left"]}
										borderColor={theme.borderDefault}
										justifyContent="center"
										alignItems="center"
									>
										<box flexDirection="column" alignItems="center">
											<text fg={theme.textSecondary}>
												Select a conversation to inspect
											</text>
											<text fg={theme.textPlaceholder}>
												Press Enter to open its transcript.
											</text>
										</box>
									</box>
								</Show>
							}
						>
							{(conversation) => (
								<box
									ref={(element) => setTranscriptTarget(element as Renderable)}
									flexGrow={1}
									flexDirection="column"
									focusable
									focused={mode() === "transcript"}
									border={wide() ? ["left"] : undefined}
									borderColor={theme.borderDefault}
									focusedBorderColor={theme.borderDefault}
									onMouseUp={() => setMode("transcript")}
								>
									<box
										flexShrink={0}
										flexDirection="row"
										paddingX={1}
										border={["bottom"]}
										borderColor={theme.borderDefault}
										justifyContent="space-between"
									>
										<text fg={theme.textPrimary}>
											{conversation().agentName}
										</text>
										<text fg={statusIndicator(conversation().status).color}>
											{statusIndicator(conversation().status).glyph}{" "}
											{statusLabel(conversation().status)}
										</text>
									</box>
									<Show
										when={!entries.loading || entries()}
										fallback={
											<text fg={theme.textMuted} paddingX={1}>
												Loading transcript…
											</text>
										}
									>
										<Show
											when={!entries.error}
											fallback={
												<text fg={theme.errorText} paddingX={1}>
													Transcript unavailable: {String(entries.error)}
												</text>
											}
										>
											<TranscriptView
												conversation={conversation()}
												entries={entries() ?? []}
												setScrollRef={(ref) => {
													transcriptScrollRef = ref;
												}}
											/>
										</Show>
									</Show>
								</box>
							)}
						</Show>
					</box>
				</Show>
			</Dialog.Body>

			<Dialog.Footer>
				<KeymapHintBar
					borderless
					group={mode() === "transcript" ? "subagent-transcript" : "subagents"}
				/>
			</Dialog.Footer>

			<Show when={mode() === "confirmDismiss" && selectedConversation()}>
				{(conversation) => (
					<Dialog.Root maxWidth={80} paddingBottom={0}>
						<Dialog.Header>
							<Dialog.Title fg={theme.errorText}>
								Dismiss "{conversation().agentName}"?
							</Dialog.Title>
						</Dialog.Header>
						<box flexDirection="column">
							<text fg={theme.textPrimary}>
								The transcript and conversation context will be deleted.
							</text>
							<Show when={conversation().status === "running"}>
								<text fg={theme.warningText}>
									The running execution will also be aborted.
								</text>
							</Show>
							<Show when={dismissError()}>
								{(error) => <text fg={theme.errorText}>{error()}</text>}
							</Show>
							<Show when={dismissing()}>
								<text fg={theme.textMuted}>Dismissing…</text>
							</Show>
						</box>
						<Dialog.Footer>
							<KeymapHintBar borderless group="subagent-dismiss" />
						</Dialog.Footer>
					</Dialog.Root>
				)}
			</Show>
		</Dialog.Root>
	);
}
