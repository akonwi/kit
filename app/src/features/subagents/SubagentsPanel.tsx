/**
 * Persistent workspace-panel presentation for sub-agent status.
 *
 * Single-column drill-in: a roster of agents/conversations, with Enter
 * (or click) opening the selected conversation's live transcript in
 * place. Destructive dismissal routes through a centered overlay dialog.
 */

import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	For,
	onCleanup,
	Show,
} from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import { CHEVRON_RIGHT, MIDDLE_DOT } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { scrollbarStyle, theme } from "../../shell/theme";
import type { OpenOverlay } from "../../shell/transcript/types";
import { WorkspacePanelLayout } from "../../shell/WorkspacePanelLayout";
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
import type { SubagentsPanelData } from "./workspace-controller";

export const SUBAGENTS_MIN_COLS = 32;

type PanelView = "roster" | "transcript";

export type SubagentsPanelProps = {
	data: SubagentsPanelData;
	active?: boolean;
	onClose: () => void;
	onFocusRequest?: () => void;
	openOverlay: OpenOverlay;
};

function DismissDialog(props: {
	overlay: OverlayComponentProps<boolean>;
	agentName: string;
	running: boolean;
	dismiss: () => Promise<boolean>;
}) {
	const [dismissing, setDismissing] = createSignal(false);
	const [error, setError] = createSignal<string | null>(null);

	async function confirm(): Promise<void> {
		if (dismissing()) return;
		setDismissing(true);
		setError(null);
		try {
			const dismissed = await props.dismiss();
			if (!dismissed) {
				setError("Conversation is no longer active.");
				setDismissing(false);
				return;
			}
			props.overlay.done(true);
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : String(cause));
			setDismissing(false);
		}
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.overlay.active !== false,
		commands: {
			"subagents.confirm-dismiss": () => void confirm(),
			"subagents.cancel-dismiss": () => {
				if (!dismissing()) props.overlay.done(false);
			},
		},
	}));

	return (
		<Dialog.Root
			maxWidth={80}
			paddingBottom={0}
			surfaceProps={props.overlay.surfaceProps}
		>
			<Dialog.Header>
				<Dialog.Title fg={theme.errorText}>
					Dismiss "{props.agentName}"?
				</Dialog.Title>
			</Dialog.Header>
			<box flexDirection="column">
				<text fg={theme.textPrimary}>
					The transcript and conversation context will be deleted.
				</text>
				<Show when={props.running}>
					<text fg={theme.warningText}>
						The running execution will also be aborted.
					</text>
				</Show>
				<Show when={error()}>
					{(message) => <text fg={theme.errorText}>{message()}</text>}
				</Show>
				<Show when={dismissing()}>
					<text fg={theme.textMuted}>Dismissing…</text>
				</Show>
			</box>
			<Dialog.Footer>
				<KeymapHintBar borderless group="subagent-dismiss" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}

export function SubagentsPanel(props: SubagentsPanelProps) {
	const [selectedName, setSelectedName] = createSignal("");
	const [view, setView] = createSignal<PanelView>("roster");
	const [revision, setRevision] = createSignal(0);
	const [clock, setClock] = createSignal(0);
	let listScrollRef:
		| { scrollTo: (opts: { x?: number; y?: number } | number) => void }
		| undefined;
	let transcriptScrollRef:
		| {
				scrollBy: (opts: { x: number; y: number }) => void;
				scrollTo: (opts: { x?: number; y?: number } | number) => void;
		  }
		| undefined;

	let closeDismissOverlay: (() => void) | null = null;
	const timer = setInterval(() => setClock((value) => value + 1), 30_000);
	const unsubscribe = props.data.subscribeToChanges(() =>
		setRevision((value) => value + 1),
	);
	onCleanup(() => {
		clearInterval(timer);
		unsubscribe();
		// Do not leave a dismiss confirmation bound to disposed providers.
		closeDismissOverlay?.();
	});

	const active = () => props.active !== false;
	const items = createMemo(() => {
		revision();
		return mergeItems(
			props.data.getAgents(),
			props.data.getActiveConversations(),
		);
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
	const detailConversation = createMemo(
		() => (view() === "transcript" ? selectedConversation() : null),
		undefined,
		{ equals: false },
	);
	const transcriptKey = createMemo(
		() => {
			const conversation = selectedConversation();
			return conversation && view() === "transcript"
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
		props.data.readConversationEntries(id),
	);
	const runningCount = createMemo(
		() => items().filter((item) => item.status === "running").length,
	);
	const conversationCount = createMemo(
		() => items().filter((item) => item.conversation).length,
	);

	createEffect(() => {
		const allItems = items();
		if (allItems.length === 0) {
			setSelectedName("");
			return;
		}
		if (!allItems.some((item) => item.name === selectedName())) {
			setSelectedName(allItems[0]?.name ?? "");
			// The selection moved involuntarily (dismissal, hydrate) — never
			// silently swap the transcript to a different agent.
			if (view() !== "roster") setView("roster");
		}
		if (!selectedConversation() && view() !== "roster") setView("roster");
	});

	createEffect(() => {
		const index = selectedIndex();
		if (index < 0) return;
		// Rows are 3 lines; the injected "Available" section header adds one.
		const headerIndex = items().findIndex((item) => item.status === "inactive");
		const headerOffset = headerIndex >= 0 && index >= headerIndex ? 1 : 0;
		listScrollRef?.scrollTo({
			x: 0,
			y: Math.max(0, index * 3 + headerOffset - 3),
		});
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
		if (selectedConversation()) setView("transcript");
	}

	function beginDismiss() {
		const conversation = selectedConversation();
		if (!conversation) return;
		void props
			.openOverlay<boolean>((overlay) => {
				closeDismissOverlay = () => overlay.done(false);
				return (
					<DismissDialog
						overlay={overlay}
						agentName={conversation.agentName}
						running={conversation.status === "running"}
						dismiss={() =>
							props.data.dismissConversation(conversation.agentName)
						}
					/>
				);
			})
			.then(() => {
				closeDismissOverlay = null;
				setRevision((value) => value + 1);
			});
	}

	useKeymapLayer(() => ({
		scope: "panel",
		when: () => active() && view() === "roster",
		commands: {
			"subagents.close": props.onClose,
			"subagents.move-up": () => moveSelection(-1),
			"subagents.move-down": () => moveSelection(1),
		},
	}));

	useKeymapLayer(() => ({
		scope: "panel",
		when: () =>
			active() && view() === "roster" && Boolean(selectedConversation()),
		commands: {
			"subagents.open": openTranscript,
			"subagents.dismiss": beginDismiss,
		},
	}));

	useKeymapLayer(() => ({
		scope: "panel",
		when: () => active() && view() === "transcript",
		commands: {
			"subagents.back": () => {
				setView("roster");
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

	const headerStatus = (conversation: ActiveSubagentConversationState) =>
		statusIndicator(conversation.status);

	return (
		<box
			width="100%"
			height="100%"
			onMouseDown={(event) => {
				if (event.button === 0) props.onFocusRequest?.();
			}}
		>
			<WorkspacePanelLayout
				header={
					<box
						flexShrink={0}
						flexDirection="row"
						justifyContent="space-between"
						gap={1}
						paddingX={1}
						border={["bottom"]}
						borderColor={theme.borderDefault}
					>
						<Show
							when={detailConversation()}
							fallback={
								<>
									<text fg={theme.textPrimary}>Sub-agents</text>
									<text fg={theme.textMuted} flexShrink={0}>
										{runningCount() > 0
											? `${runningCount()} running ${MIDDLE_DOT} ${conversationCount()} conversations`
											: `${conversationCount()} conversations`}
									</text>
								</>
							}
						>
							{(conversation) => (
								<>
									<text fg={theme.textPrimary} truncate>
										{conversation().agentName}
									</text>
									<text fg={headerStatus(conversation()).color} flexShrink={0}>
										{headerStatus(conversation()).glyph}{" "}
										{statusLabel(conversation().status)}
									</text>
								</>
							)}
						</Show>
					</box>
				}
				footer={
					<KeymapHintBar
						borderless
						group={
							view() === "transcript" ? "subagent-transcript" : "subagents"
						}
					/>
				}
			>
				<Show when={items().length > 0} fallback={<SubagentsEmptyState />}>
					<Show
						when={detailConversation()}
						fallback={
							<scrollbox
								ref={(element) => {
									listScrollRef = element as typeof listScrollRef;
								}}
								flexGrow={1}
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
															if (item.conversation) setView("transcript");
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
															<text fg={indicator().color} flexShrink={0}>
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
						}
					>
						{(conversation) => (
							<box flexGrow={1} flexDirection="column">
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
											setScrollRef={(ref) => {
												transcriptScrollRef = ref;
											}}
										/>
									</Show>
								</Show>
							</box>
						)}
					</Show>
				</Show>
			</WorkspacePanelLayout>
		</box>
	);
}
