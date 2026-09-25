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
import type { SessionEntry } from "../../session";
import { Dialog } from "../../shell/Dialog";
import { CHEVRON_RIGHT, MIDDLE_DOT } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { scrollbarStyle, theme } from "../../shell/theme";
import type { OpenImage } from "../../shell/transcript/types";
import type { SubagentDefinition } from "./discovery";
import {
	mergeItems,
	relativeTime,
	SubagentsEmptyState,
	SubagentTranscriptView,
	sourceLabel,
	statusIndicator,
	statusLabel,
} from "./presentation";
import type { ActiveSubagentConversationState } from "./state";

type ViewMode = "list" | "transcript" | "confirmDismiss";

export type SubagentsStatusModalProps = {
	surfaceProps?: OverlaySurfaceProps;
	getAgents: () => SubagentDefinition[];
	getActiveConversations: () => ActiveSubagentConversationState[];
	readConversationEntries: (conversationId: string) => Promise<SessionEntry[]>;
	subscribeToChanges: (listener: () => void) => () => void;
	dismissConversation: (agentName: string) => Promise<boolean>;
	openImage: OpenImage;
	active?: boolean;
	onClose: () => void;
};

const SUBAGENT_LIST_WIDTH = 36;

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
			padding={0}
			gap={0}
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
			<Dialog.Header strip>
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

			<Dialog.Body overflow="hidden">
				<Show when={items().length > 0} fallback={<SubagentsEmptyState />}>
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
											<SubagentTranscriptView
												conversation={conversation()}
												entries={entries() ?? []}
												openImage={props.openImage}
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

			<Dialog.Footer strip>
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
