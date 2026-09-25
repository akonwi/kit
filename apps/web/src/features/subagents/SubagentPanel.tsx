/** Retained workspace pane for one sub-agent conversation. */

import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	onCleanup,
	Show,
} from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import type { OpenImage, OpenOverlay } from "../../shell/transcript/types";
import {
	WorkspacePanelHeader,
	WorkspacePanelLayout,
} from "../../shell/WorkspacePanelLayout";
import {
	SubagentTranscriptView,
	statusIndicator,
	statusLabel,
} from "./presentation";
import { SubagentDismissDialog } from "./SubagentDismissDialog";
import type { ActiveSubagentConversationState } from "./state";
import type { SubagentsPanelData } from "./workspace-controller";

export type SubagentPanelProps = {
	agentName: string;
	data: () => SubagentsPanelData | null;
	active?: boolean;
	onBack: () => void;
	onDismissed: () => void;
	onFocusRequest?: () => void;
	openImage: OpenImage;
	openOverlay: OpenOverlay;
};

export function SubagentPanel(props: SubagentPanelProps) {
	const [revision, setRevision] = createSignal(0);
	let transcriptScrollRef:
		| {
				scrollBy: (opts: { x: number; y: number }) => void;
				scrollTo: (opts: { x?: number; y?: number } | number) => void;
		  }
		| undefined;
	let closeDismissOverlay: (() => void) | null = null;
	let dismissalConfirming = false;
	onCleanup(() => {
		if (!dismissalConfirming) closeDismissOverlay?.();
	});
	createEffect(() => {
		const data = props.data();
		if (!data) return;
		onCleanup(data.subscribeToChanges(() => setRevision((value) => value + 1)));
	});

	const active = () => props.active !== false;
	const conversation = createMemo(
		() => {
			revision();
			return (
				props
					.data()
					?.getActiveConversations()
					.find((item) => item.agentName === props.agentName) ?? null
			);
		},
		undefined,
		{ equals: false },
	);
	const transcriptKey = createMemo(
		() => {
			const data = props.data();
			const current = conversation();
			return data && current
				? ([
						data,
						current.subagentConversationId,
						current.transcriptRevision ?? 0,
					] as const)
				: undefined;
		},
		undefined,
		{
			equals: (previous, next) =>
				previous?.[0] === next?.[0] &&
				previous?.[1] === next?.[1] &&
				previous?.[2] === next?.[2],
		},
	);
	const [entries] = createResource(transcriptKey, async ([data, id]) =>
		data.readConversationEntries(id),
	);

	function beginDismiss(current: ActiveSubagentConversationState) {
		void props
			.openOverlay<boolean>((overlay) => {
				closeDismissOverlay = () => overlay.done(false);
				return (
					<SubagentDismissDialog
						overlay={overlay}
						agentName={current.agentName}
						running={current.status === "running"}
						dismiss={() =>
							props.data()?.dismissConversation(current.agentName) ??
							Promise.resolve(false)
						}
						onConfirmingChange={(confirming) => {
							dismissalConfirming = confirming;
						}}
					/>
				);
			})
			.then((dismissed) => {
				closeDismissOverlay = null;
				if (dismissed) props.onDismissed();
			});
	}

	useKeymapLayer(() => ({
		scope: "panel",
		when: active,
		commands: {
			"subagents.back": props.onBack,
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
			"subagents.dismiss-transcript": () => {
				const current = conversation();
				if (current) beginDismiss(current);
			},
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
			<Show
				when={conversation()}
				fallback={
					<WorkspacePanelLayout
						footer={<KeymapHintBar borderless group="subagent-transcript" />}
					>
						<box flexGrow={1} alignItems="center" justifyContent="center">
							<text fg={theme.textMuted}>Conversation unavailable</text>
						</box>
					</WorkspacePanelLayout>
				}
			>
				{(current) => {
					const indicator = () => statusIndicator(current().status);
					return (
						<WorkspacePanelLayout
							header={
								<WorkspacePanelHeader
									left={
										<text fg={indicator().color}>
											{indicator().glyph} {statusLabel(current().status)}
										</text>
									}
									right={
										<Show when={current().model}>
											{(model) => <text fg={theme.textMuted}>{model()}</text>}
										</Show>
									}
								/>
							}
							footer={<KeymapHintBar borderless group="subagent-transcript" />}
						>
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
										conversation={current()}
										entries={entries() ?? []}
										openImage={props.openImage}
										setScrollRef={(ref) => {
											transcriptScrollRef = ref;
										}}
									/>
								</Show>
							</Show>
						</WorkspacePanelLayout>
					);
				}}
			</Show>
		</box>
	);
}
