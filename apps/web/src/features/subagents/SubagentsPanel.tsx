/** Persistent workspace roster for available and active sub-agents. */

import {
	createEffect,
	createMemo,
	createSignal,
	For,
	onCleanup,
	Show,
} from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { CHEVRON_RIGHT, MIDDLE_DOT } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { scrollbarStyle, theme } from "../../shell/theme";
import type { OpenOverlay } from "../../shell/transcript/types";
import {
	WorkspacePanelHeader,
	WorkspacePanelLayout,
} from "../../shell/WorkspacePanelLayout";
import {
	mergeItems,
	relativeTime,
	SubagentsEmptyState,
	sourceLabel,
	statusIndicator,
	statusLabel,
} from "./presentation";
import { SubagentDismissDialog } from "./SubagentDismissDialog";
import type { SubagentsPanelData } from "./workspace-controller";

export type SubagentsPanelProps = {
	data: () => SubagentsPanelData | null;
	active?: boolean;
	onClose: () => void;
	onFocusRequest?: () => void;
	onOpenAgent: (agentName: string) => void;
	openOverlay: OpenOverlay;
};

export function SubagentsPanel(props: SubagentsPanelProps) {
	const [selectedName, setSelectedName] = createSignal("");
	const [revision, setRevision] = createSignal(0);
	const [clock, setClock] = createSignal(0);
	let listScrollRef:
		| { scrollTo: (opts: { x?: number; y?: number } | number) => void }
		| undefined;
	let closeDismissOverlay: (() => void) | null = null;
	const timer = setInterval(() => {
		if (props.active !== false) setClock((value) => value + 1);
	}, 30_000);
	onCleanup(() => {
		clearInterval(timer);
		closeDismissOverlay?.();
	});
	createEffect(() => {
		const data = props.data();
		if (!data) return;
		onCleanup(data.subscribeToChanges(() => setRevision((value) => value + 1)));
	});

	const active = () => props.active !== false;
	const items = createMemo(() => {
		revision();
		const data = props.data();
		return data
			? mergeItems(data.getAgents(), data.getActiveConversations())
			: [];
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
		}
	});

	createEffect(() => {
		const index = selectedIndex();
		if (index < 0) return;
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
		const conversation = selectedConversation();
		if (conversation) props.onOpenAgent(conversation.agentName);
	}

	function beginDismiss() {
		const conversation = selectedConversation();
		if (!conversation) return;
		void props
			.openOverlay<boolean>((overlay) => {
				closeDismissOverlay = () => overlay.done(false);
				return (
					<SubagentDismissDialog
						overlay={overlay}
						agentName={conversation.agentName}
						running={conversation.status === "running"}
						dismiss={() =>
							props.data()?.dismissConversation(conversation.agentName) ??
							Promise.resolve(false)
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
		when: active,
		commands: {
			"subagents.close": props.onClose,
			"subagents.move-up": () => moveSelection(-1),
			"subagents.move-down": () => moveSelection(1),
		},
	}));

	useKeymapLayer(() => ({
		scope: "panel",
		when: () => active() && Boolean(selectedConversation()),
		commands: {
			"subagents.open": openTranscript,
			"subagents.dismiss": beginDismiss,
		},
	}));

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
					<WorkspacePanelHeader
						left={
							<text fg={theme.textMuted}>
								{runningCount() > 0
									? `${runningCount()} running ${MIDDLE_DOT} ${conversationCount()} conversations`
									: `${conversationCount()} conversations`}
							</text>
						}
					/>
				}
				footer={<KeymapHintBar borderless group="subagents" />}
			>
				<Show when={items().length > 0} fallback={<SubagentsEmptyState />}>
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
													if (item.conversation) props.onOpenAgent(item.name);
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
				</Show>
			</WorkspacePanelLayout>
		</box>
	);
}
