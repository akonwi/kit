import { useRenderer } from "@opentui/solid";
import type { JSX } from "solid-js";
import { createEffect, createSignal, For, onCleanup, Show } from "solid-js";
import type { OverlayComponentProps } from "../app/overlay-ui";
import {
	getOverlaySurfaceProps,
	getToastStackZIndex,
	type OverlayEntry,
} from "../app/overlay-ui";
import type { Command, CommandRegistry } from "../features/commands";
import { CodeReviewAttachment } from "../features/review/attachment";
import type { ReviewDraftController } from "../features/review/draft-controller";
import {
	ReviewAttachmentDialog,
	ReviewAttachmentSidebar,
	type ReviewAttachmentSource,
	reviewAttachmentSourceEquals,
} from "../features/review/ReviewAttachmentViewer";
import { ReviewContent } from "../features/review/ReviewContent";
import type { ReviewWorkspaceController } from "../features/review/workspace-controller";
import type { ScratchpadController } from "../features/scratchpad/controller";
import {
	SCRATCHPAD_MIN_COLS,
	SCRATCHPAD_MIN_WIDTH,
	ScratchpadDialog,
	ScratchpadPanel,
} from "../features/scratchpad/ScratchpadPanel";
import { createKeybindingDiagnosticReporter } from "../keymap/diagnostics";
import { getKeybindingCommand } from "../keymap/registry";
import { KeymapLayerProvider, useKeymapLayer } from "../keymap/useKeymapLayer";
import type { AgentRuntime } from "../runtime/agent-runtime";
import {
	type ReviewDiffView,
	resolveDiffSettings,
	type Settings,
	updateSettings,
} from "../settings";
import type { AppState } from "../state/app-state";
import type { ToastInput } from "../state/toasts";
import type { AttachmentsController } from "./attachments-controller";
import {
	BottomStatusBar,
	VCS_LOCATION_CONTRIBUTION_ID,
} from "./BottomStatusBar";
import {
	ChromeOverflowPicker,
	type ChromeOverflowPlacement,
} from "./ChromeOverflowPicker";
import { CommandPalette } from "./CommandPalette";
import { ComposerDock, type ComposerInputMode } from "./ComposerDock";
import type { ChromeContribution } from "./chrome-contributions";
import type { ComposerController } from "./composer-controller";
import type { FooterStatusController } from "./footer-status";
import { HeaderBar } from "./HeaderBar";
import type { HeaderStatusController } from "./header-status";
import { InlinePicker } from "./InlinePicker";
import { PendingSlot } from "./PendingSlot";
import { QueueEditorDialog } from "./QueueEditorDialog";
import { copySelection } from "./selection";
import { ToastStack } from "./ToastStack";
import { theme } from "./theme";
import { Transcript } from "./transcript";
import { TurnActivityDialog } from "./transcript/TurnActivityDialog";
import { TurnActivitySidebar } from "./transcript/TurnActivitySidebar";
import type { ActivitySource } from "./transcript/turn-activity-view";
import type { OpenActivity, OpenOverlay } from "./transcript/types";
import { WorkspacePaneHost } from "./WorkspacePaneHost";
import {
	resolveWorkspacePaneLayout,
	WORKSPACE_MIN_PRIMARY_COLUMNS,
	WORKSPACE_MIN_SECONDARY_COLUMNS,
} from "./workspace-layout";
import {
	createWorkspaceStateController,
	type WorkspaceFocusedSurface,
	type WorkspaceState,
} from "./workspace-state";

/**
 * Terminal width (in columns) at which the turn activity view is shown as
 * a right-side sidebar alongside the transcript. Below this threshold it
 * falls back to the existing modal dialog.
 */
const ACTIVITY_SIDEBAR_MIN_WIDTH = 200;

const ACTIVITY_MIN_COLS = 40;
const REVIEW_MIN_COLS = 60;

type WorkspaceReviewSource = ReviewAttachmentSource | { kind: "editor" };

type WorkspacePane =
	| { kind: "activity"; source: ActivitySource }
	| { kind: "review"; source: WorkspaceReviewSource }
	| { kind: "scratchpad" };

export type AppShellProps = {
	settings: Settings;
	state: AppState;
	runtime: AgentRuntime;
	commands: CommandRegistry;
	controller: ComposerController;
	attachments: AttachmentsController;
	footer: FooterStatusController;
	header: HeaderStatusController;
	reviewDrafts: ReviewDraftController;
	reviewWorkspace: ReviewWorkspaceController;
	scratchpad: ScratchpadController;
	overlays: () => OverlayEntry[];
	openOverlay: OpenOverlay;
	dismissToast: (id: number) => void;
	onTranscriptViewportChange: (viewport: {
		width: number;
		height: number;
	}) => void;
	showToast: (toast: ToastInput) => void;
};

type AppShellContentProps = Omit<AppShellProps, "settings" | "showToast"> & {
	showToast: (toast: ToastInput) => void;
	preferredPaneRatio: number | undefined;
	defaultReviewDiffView: ReviewDiffView;
	onReviewDiffViewChanged: (view: ReviewDiffView) => void;
	onPreferredPaneRatioCommit: (ratio: number) => void;
};

function activitySourceEquals(a: ActivitySource, b: ActivitySource): boolean {
	if (a.kind === "single-item" && b.kind === "single-item")
		return a.itemId === b.itemId;
	if (a.kind === "turn-intermediate" && b.kind === "turn-intermediate")
		return a.turnId === b.turnId;
	return false;
}

function commandKeybindingGroup(command: Command): string {
	if (command.category) return command.category;
	const dot = command.name.indexOf(".");
	return dot > 0 ? command.name.slice(0, dot) : "Commands";
}

export function shouldRestoreComposerFocus(options: {
	overlayOpen: boolean;
	chromeOverflowOpen: boolean;
	pickerVisible: boolean;
	commandPaletteVisible: boolean;
	focusedSurface: WorkspaceFocusedSurface;
}): boolean {
	return (
		!options.overlayOpen &&
		!options.chromeOverflowOpen &&
		!options.pickerVisible &&
		!options.commandPaletteVisible &&
		options.focusedSurface === "composer"
	);
}

export function shouldHandleScratchpadFocusNext(options: {
	scratchpadOpen: boolean;
	overlayOpen: boolean;
	pickerVisible: boolean;
	commandPaletteVisible: boolean;
}): boolean {
	return (
		options.scratchpadOpen &&
		!options.overlayOpen &&
		!options.pickerVisible &&
		!options.commandPaletteVisible
	);
}

function AppShellContent(props: AppShellContentProps) {
	const [headerHeight, setHeaderHeight] = createSignal(1);
	const [dockHeight, setDockHeight] = createSignal(3);
	const [composerMode, setComposerMode] =
		createSignal<ComposerInputMode>("normal");
	const [commandRegistryVersion, setCommandRegistryVersion] = createSignal(0);
	const renderer = useRenderer();
	let transcriptRef: { width: number; height: number } | undefined;
	const [transcriptWidth, setTranscriptWidth] = createSignal(
		renderer.terminalWidth,
	);

	// Track outer terminal width so we can switch the turn activity view
	// between sidebar (wide) and modal (narrow) modes responsively.
	const [shellWidth, setShellWidth] = createSignal(renderer.terminalWidth);
	let shellRef: { width: number; height: number } | undefined;

	const workspace = createWorkspaceStateController<WorkspacePane>({
		preferredPaneRatio: props.preferredPaneRatio,
	});
	const [workspaceState, setWorkspaceState] = createSignal<
		WorkspaceState<WorkspacePane>
	>(workspace.getState());
	onCleanup(workspace.subscribe(setWorkspaceState));
	const activeSecondaryPane = () => {
		const secondary = workspaceState().secondary;
		return secondary.status === "empty" ? null : secondary.pane;
	};
	const openSecondaryPane = () => {
		const secondary = workspaceState().secondary;
		return secondary.status === "open" ? secondary.pane : null;
	};
	const focusedSurface = () => workspaceState().focusedSurface;
	function focusComposerSurface(): void {
		workspace.setNarrowTab("transcript");
		workspace.setFocusedSurface("composer");
	}
	function focusReviewSurface(): void {
		props.controller.picker.clear();
		workspace.setNarrowTab("review");
		workspace.setFocusedSurface("secondary");
	}
	onCleanup(
		props.reviewWorkspace.subscribe(() => {
			saveScratchpadDraftIfEditing();
			workspace.openSecondary({ kind: "review", source: { kind: "editor" } });
			focusReviewSurface();
		}),
	);
	createEffect(() => {
		if (props.preferredPaneRatio !== undefined) {
			workspace.setPreferredPaneRatio(props.preferredPaneRatio);
		}
	});
	const [chromeOverflow, setChromeOverflow] = createSignal<{
		title: string;
		placement: ChromeOverflowPlacement;
	} | null>(null);
	const [chromeContributionVersion, setChromeContributionVersion] =
		createSignal(0);
	onCleanup(
		props.header.subscribe(() =>
			setChromeContributionVersion((version) => version + 1),
		),
	);
	onCleanup(
		props.footer.subscribe(() =>
			setChromeContributionVersion((version) => version + 1),
		),
	);

	function currentOverflowContributions(): readonly ChromeContribution[] {
		void chromeContributionVersion();
		const overflow = chromeOverflow();
		if (!overflow) return [];
		if (overflow.placement === "header") {
			return props.header.getContributions();
		}
		return props.footer
			.getContributions()
			.filter((item) => item.id !== VCS_LOCATION_CONTRIBUTION_ID);
	}

	function openChromeOverflow(
		title: string,
		placement: ChromeOverflowPlacement,
		contributions: readonly ChromeContribution[],
	) {
		if (contributions.length === 0) return;
		if (props.overlays().length > 0) return;
		if (props.controller.picker.visible) return;
		if (props.controller.commandPalette.visible) return;
		setChromeOverflow({ title, placement });
	}

	createEffect(() => {
		if (!chromeOverflow()) return;
		if (
			currentOverflowContributions().length === 0 ||
			props.overlays().length > 0 ||
			props.controller.picker.visible ||
			props.controller.commandPalette.visible
		) {
			setChromeOverflow(null);
		}
	});

	const activityWideEnough = () => shellWidth() >= ACTIVITY_SIDEBAR_MIN_WIDTH;
	const scratchpadWideEnough = () => shellWidth() >= SCRATCHPAD_MIN_WIDTH;
	const sidebarSource = () => {
		const panel = openSecondaryPane();
		return panel?.kind === "activity" && activityWideEnough()
			? panel.source
			: null;
	};
	const reviewSidebarSource = () => {
		const panel = openSecondaryPane();
		return panel?.kind === "review" &&
			panel.source.kind !== "editor" &&
			activityWideEnough()
			? panel.source
			: null;
	};
	const editableReviewOpen = () => {
		const panel = openSecondaryPane();
		return panel?.kind === "review" && panel.source.kind === "editor";
	};
	const resolveReviewSource = (source: ReviewAttachmentSource) => {
		if (source.kind === "historical") {
			return { draft: false, review: source.review };
		}
		const attachment = props.attachments
			.attachments()
			.find((candidate) => candidate.id === source.attachmentId);
		return attachment instanceof CodeReviewAttachment
			? { draft: true, review: attachment.review }
			: null;
	};
	const scratchpadOpen = () =>
		openSecondaryPane()?.kind === "scratchpad" && scratchpadWideEnough();
	const secondaryPaneVisible = () =>
		editableReviewOpen() ||
		sidebarSource() !== null ||
		reviewSidebarSource() !== null ||
		scratchpadOpen();
	const secondaryPaneMinColumns = () => {
		const pane = openSecondaryPane();
		if (pane?.kind === "scratchpad") return SCRATCHPAD_MIN_COLS;
		if (pane?.kind === "review" && pane.source.kind === "editor") {
			return REVIEW_MIN_COLS;
		}
		return ACTIVITY_MIN_COLS;
	};
	const reviewUsesNarrowTabs = () =>
		editableReviewOpen() &&
		resolveWorkspacePaneLayout({
			availableColumns: Math.max(0, shellWidth() - 2),
			preferredPaneRatio: workspaceState().preferredPaneRatio,
			minPrimaryColumns: WORKSPACE_MIN_PRIMARY_COLUMNS,
			minSecondaryColumns: REVIEW_MIN_COLS,
		}) === null;

	// Responsive presentation must not overwrite whether the user considers a
	// pane open. Preserve workspace state while narrow and offer the scratchpad
	// dialog once when an active inline editor can no longer be presented.
	let openedResponsiveScratchpadDialog = false;
	createEffect(() => {
		const panel = openSecondaryPane();
		if (panel?.kind !== "scratchpad" || scratchpadWideEnough()) {
			openedResponsiveScratchpadDialog = false;
			return;
		}
		if (props.scratchpad.editing() && !openedResponsiveScratchpadDialog) {
			openedResponsiveScratchpadDialog = true;
			openScratchpadDialog();
		}
	});

	createEffect(() => {
		const panel = activeSecondaryPane();
		if (
			panel?.kind === "review" &&
			panel.source.kind === "draft" &&
			!resolveReviewSource(panel.source)
		) {
			workspace.clearSecondary();
		}
	});

	onCleanup(
		props.runtime.subscribe("session.active.changed", () => {
			const panel = activeSecondaryPane();
			if (panel?.kind === "review" || panel?.kind === "activity") {
				workspace.clearSecondary();
			}
		}),
	);

	const openQueueEditor = () => {
		if (props.runtime.getPendingMessageCount() === 0) {
			props.showToast({
				title: "No queued messages",
				subtitle:
					"Queue a follow-up while the agent is working to edit it here.",
				variant: "info",
			});
			return;
		}
		void props.openOverlay(
			(overlayProps: OverlayComponentProps<void>): JSX.Element => (
				<QueueEditorDialog
					runtime={props.runtime}
					done={overlayProps.done}
					surfaceProps={overlayProps.surfaceProps}
					active={overlayProps.active}
				/>
			),
		);
	};

	function saveScratchpadDraftIfEditing(): void {
		if (props.scratchpad.editing()) props.scratchpad.autosaveDraft();
	}

	const openActivity: OpenActivity = (source) => {
		if (shellWidth() >= ACTIVITY_SIDEBAR_MIN_WIDTH && !editableReviewOpen()) {
			// Re-clicking the same chip while its sidebar is open is a no-op;
			// a different chip swaps the content.
			const current = openSecondaryPane();
			if (
				current?.kind === "activity" &&
				activitySourceEquals(current.source, source)
			) {
				return;
			}
			saveScratchpadDraftIfEditing();
			workspace.openSecondary(
				{ kind: "activity", source },
				{ focus: "composer" },
			);
			return;
		}
		if (!editableReviewOpen()) {
			saveScratchpadDraftIfEditing();
			workspace.setActiveSecondary({ kind: "activity", source });
			workspace.setFocusedSurface("composer");
		}
		void props.openOverlay(
			(overlayProps: OverlayComponentProps<unknown>): JSX.Element => (
				<TurnActivityDialog
					runtime={props.runtime}
					source={source}
					done={overlayProps.done}
					surfaceProps={overlayProps.surfaceProps}
					active={overlayProps.active}
				/>
			),
		);
	};

	function editReviewDraft(): void {
		workspace.minimizeSecondary();
		const command = props.commands
			.getAll()
			.find((candidate) => candidate.name === "code-review");
		if (!command) return;
		queueMicrotask(() => {
			void props.controller.runCommand(command, "");
		});
	}

	const openReviewAttachment = (source: ReviewAttachmentSource) => {
		const resolved = resolveReviewSource(source);
		if (!resolved) return;
		if (source.kind === "draft") {
			props.reviewWorkspace.open();
			return;
		}
		if (activityWideEnough() && !editableReviewOpen()) {
			const current = openSecondaryPane();
			if (
				current?.kind === "review" &&
				current.source.kind !== "editor" &&
				reviewAttachmentSourceEquals(current.source, source)
			) {
				return;
			}
			saveScratchpadDraftIfEditing();
			workspace.openSecondary(
				{ kind: "review", source },
				{ focus: "composer" },
			);
			return;
		}
		if (!editableReviewOpen()) {
			saveScratchpadDraftIfEditing();
			workspace.setActiveSecondary({ kind: "review", source });
			workspace.setFocusedSurface("composer");
		}
		void props.openOverlay(
			(overlayProps: OverlayComponentProps<void>): JSX.Element => (
				<ReviewAttachmentDialog
					review={resolved.review}
					draft={resolved.draft}
					cwd={props.runtime.getSession().cwd}
					onEdit={resolved.draft ? editReviewDraft : undefined}
					done={overlayProps.done}
					surfaceProps={overlayProps.surfaceProps}
					active={overlayProps.active}
				/>
			),
		);
	};

	const openScratchpadDialog = (
		returnFocus: "composer" | "secondary" = "composer",
	) => {
		workspace.setFocusedSurface(returnFocus);
		void props.openOverlay(
			(overlayProps: OverlayComponentProps<void>): JSX.Element => (
				<ScratchpadDialog
					controller={props.scratchpad}
					done={(result) => {
						workspace.setFocusedSurface(returnFocus);
						overlayProps.done(result);
					}}
					surfaceProps={overlayProps.surfaceProps}
					active={overlayProps.active}
				/>
			),
		);
	};

	function cycleInputFocus(): void {
		workspace.setFocusedSurface(
			focusedSurface() === "secondary" ? "composer" : "secondary",
		);
	}

	const toggleScratchpad = () => {
		if (editableReviewOpen()) {
			openScratchpadDialog("secondary");
			return;
		}
		const panel = openSecondaryPane();
		if (panel?.kind === "scratchpad") {
			saveScratchpadDraftIfEditing();
			workspace.minimizeSecondary();
			return;
		}
		if (scratchpadWideEnough()) {
			workspace.openSecondary({ kind: "scratchpad" }, { focus: "secondary" });
			return;
		}
		workspace.setActiveSecondary({ kind: "scratchpad" });
		openScratchpadDialog();
	};

	onCleanup(
		props.commands.register({
			name: "toggle-scratchpad",
			description: "Toggle scratchpad",
			category: "App",
			execute: () => {
				toggleScratchpad();
			},
		}),
	);

	onCleanup(
		props.commands.subscribe(() => {
			setCommandRegistryVersion((version) => version + 1);
		}),
	);

	useKeymapLayer(() => {
		commandRegistryVersion();
		const bindableCommands = props.commands
			.getAll()
			.filter((command) => !getKeybindingCommand(command.name));
		return {
			scope: "app",
			when: () => props.overlays().length === 0 && chromeOverflow() === null,
			commandMetadata: Object.fromEntries(
				bindableCommands.map((command) => [
					command.name,
					{
						defaultKeys: [],
						desc: command.description,
						group: commandKeybindingGroup(command),
						hint: false,
					},
				]),
			),
			commands: {
				"command-palette.open": () => {
					props.controller.openCommandPalette();
				},
				"queue-editor.open": openQueueEditor,
				"scratchpad.focus-next": () => {
					if (
						!shouldHandleScratchpadFocusNext({
							scratchpadOpen: scratchpadOpen(),
							overlayOpen: props.overlays().length > 0,
							pickerVisible: props.controller.picker.visible,
							commandPaletteVisible: props.controller.commandPalette.visible,
						})
					) {
						return false;
					}
					cycleInputFocus();
				},
			},
			generatedCommands: Object.fromEntries(
				bindableCommands.map((command) => [
					command.name,
					() => {
						if (props.controller.picker.visible) return false;
						if (props.controller.commandPalette.visible) return false;
						void props.controller.runCommand(command, "");
					},
				]),
			),
		};
	});

	return (
		<box
			width="100%"
			height="100%"
			border
			borderColor={theme.borderDefault}
			flexDirection="column"
			backgroundColor={theme.bg}
			onMouseDown={() => {
				if (chromeOverflow()) setChromeOverflow(null);
			}}
			onMouseUp={() => copySelection(renderer)}
			ref={(value) => {
				shellRef = value as typeof shellRef;
			}}
			onSizeChange={() => {
				if (shellRef) setShellWidth(shellRef.width);
			}}
		>
			<HeaderBar
				runtime={props.runtime}
				header={props.header}
				sessionName={props.state.sessionMeta.name}
				shellWidth={shellWidth()}
				transcriptWidth={transcriptWidth()}
				showContextProgress={
					!reviewUsesNarrowTabs() || workspaceState().narrowTab === "transcript"
				}
				onHeightChange={setHeaderHeight}
				onOpenOverflow={(contributions) =>
					openChromeOverflow("Header status", "header", contributions)
				}
				onOverflowAvailabilityChange={(available) => {
					if (!available && chromeOverflow()?.placement === "header") {
						setChromeOverflow(null);
					}
				}}
			/>

			{/*
			 * Main row sits between the full-width HeaderBar and
			 * BottomStatusBar. The left column holds the transcript + the
			 * composer stack (pending slot, composer dock); when the
			 * activity sidebar is open it mounts to the right and extends
			 * the full height of this row so pending/composer UI no longer
			 * bleeds under it.
			 */}
			<WorkspacePaneHost
				secondaryOpen={secondaryPaneVisible()}
				initialWidth={Math.max(1, shellWidth() - 2)}
				preferredPaneRatio={workspaceState().preferredPaneRatio}
				minPrimaryColumns={WORKSPACE_MIN_PRIMARY_COLUMNS}
				minSecondaryColumns={Math.max(
					WORKSPACE_MIN_SECONDARY_COLUMNS,
					secondaryPaneMinColumns(),
				)}
				onPreferredPaneRatioChange={(ratio) =>
					workspace.setPreferredPaneRatio(ratio)
				}
				onPreferredPaneRatioCommit={props.onPreferredPaneRatioCommit}
				onDividerMouseDown={() => renderer.clearSelection()}
				narrowTabs={
					editableReviewOpen()
						? {
								selected: () => workspaceState().narrowTab,
								onSelect: (tab) => {
									if (tab === "review") focusReviewSurface();
									else focusComposerSurface();
								},
							}
						: undefined
				}
				secondary={
					<>
						<Show when={editableReviewOpen()}>
							<box
								position="relative"
								width="100%"
								height="100%"
								border={reviewUsesNarrowTabs() ? false : ["left"]}
								borderColor={theme.borderDefault}
								onMouseDown={(event) => {
									if (event.button === 0) focusReviewSurface();
								}}
							>
								<ReviewContent
									onClose={() => {
										workspace.minimizeSecondary();
										workspace.setNarrowTab("transcript");
										workspace.setFocusedSurface("composer");
									}}
									attachments={props.attachments}
									reviewDrafts={props.reviewDrafts}
									toast={props.showToast}
									defaultDiffView={props.defaultReviewDiffView}
									onDiffViewChanged={props.onReviewDiffViewChanged}
									presentation="pane"
									closeOnSubmit={false}
									onFocusRequest={focusReviewSurface}
									active={
										focusedSurface() === "secondary" &&
										props.overlays().length === 0 &&
										chromeOverflow() === null &&
										!props.controller.picker.visible &&
										!props.controller.commandPalette.visible
									}
								/>
							</box>
						</Show>
						{/* `keyed` so swapping activity sources remounts the model,
						 * which captures its source statically at creation. */}
						<Show keyed when={sidebarSource()}>
							{(source) => (
								<TurnActivitySidebar
									runtime={props.runtime}
									source={source}
									onClose={() => workspace.minimizeSecondary()}
								/>
							)}
						</Show>
						<Show keyed when={reviewSidebarSource()}>
							{(source) => (
								<Show when={resolveReviewSource(source)}>
									{(resolved) => (
										<ReviewAttachmentSidebar
											review={resolved().review}
											draft={resolved().draft}
											cwd={props.runtime.getSession().cwd}
											onEdit={resolved().draft ? editReviewDraft : undefined}
											onClose={() => workspace.minimizeSecondary()}
										/>
									)}
								</Show>
							)}
						</Show>
						<Show when={scratchpadOpen()}>
							<ScratchpadPanel
								controller={props.scratchpad}
								active={
									props.overlays().length === 0 &&
									chromeOverflow() === null &&
									focusedSurface() === "secondary"
								}
								onClose={() => {
									workspace.minimizeSecondary();
									workspace.setFocusedSurface("composer");
								}}
								onFocusRequest={() => workspace.setFocusedSurface("secondary")}
							/>
						</Show>
					</>
				}
			>
				<box
					flexGrow={1}
					flexDirection="column"
					onMouseUp={(event) => {
						if (event.button !== 0) return;
						if (renderer.getSelection()?.getSelectedText()) return;
						focusComposerSurface();
						queueMicrotask(() => {
							if (
								!shouldRestoreComposerFocus({
									overlayOpen: props.overlays().length > 0,
									chromeOverflowOpen: chromeOverflow() !== null,
									pickerVisible: props.controller.picker.visible,
									commandPaletteVisible:
										props.controller.commandPalette.visible,
									focusedSurface: focusedSurface(),
								})
							) {
								return;
							}
							props.controller.focusTextarea();
						});
					}}
				>
					<box
						flexGrow={1}
						ref={(value) => {
							transcriptRef = value as typeof transcriptRef;
						}}
						onSizeChange={() => {
							if (!transcriptRef) return;
							setTranscriptWidth(transcriptRef.width);
							props.onTranscriptViewportChange({
								width: transcriptRef.width,
								height: transcriptRef.height,
							});
						}}
					>
						<Transcript
							runtime={props.runtime}
							showToast={props.showToast}
							openOverlay={props.openOverlay}
							openActivity={openActivity}
							openReviewAttachment={openReviewAttachment}
						/>
					</box>
					<box flexShrink={0} flexDirection="column" gap={0}>
						<PendingSlot
							runtime={props.runtime}
							pendingMessages={props.state.pendingMessages}
						/>
						<ComposerDock
							controller={props.controller}
							attachments={props.attachments}
							onOpenAttachment={(attachment) => {
								if (attachment instanceof CodeReviewAttachment) {
									openReviewAttachment({
										kind: "draft",
										attachmentId: attachment.id,
									});
									return;
								}
								void Promise.resolve(attachment.onOpen?.()).catch((error) => {
									props.showToast({
										title: "Could not open attachment",
										subtitle:
											error instanceof Error ? error.message : String(error),
										variant: "error",
									});
								});
							}}
							locked={props.overlays().length > 0 || chromeOverflow() !== null}
							inputFocused={
								chromeOverflow() === null &&
								(focusedSurface() === "composer" || props.overlays().length > 0)
							}
							onHeightChange={setDockHeight}
							onModeChange={setComposerMode}
							onFocusRequest={focusComposerSurface}
						/>
					</box>
					{/* Inline @/# reference picker is constrained to the primary
					 * workspace column and floats above the composer dock. */}
					<InlinePicker
						picker={props.controller.picker}
						bottomOffset={dockHeight() + 2}
					/>
				</box>
			</WorkspacePaneHost>

			<BottomStatusBar
				runtime={props.runtime}
				status={props.footer}
				composerMode={composerMode()}
				shellWidth={shellWidth()}
				onOpenOverflow={(contributions) =>
					openChromeOverflow("Footer status", "footer", contributions)
				}
				onOverflowAvailabilityChange={(available) => {
					if (!available && chromeOverflow()?.placement === "footer") {
						setChromeOverflow(null);
					}
				}}
			/>

			<Show when={chromeOverflow()}>
				{(overflow) => (
					<ChromeOverflowPicker
						title={overflow().title}
						placement={overflow().placement}
						contributions={currentOverflowContributions()}
						width={Math.max(1, Math.min(72, shellWidth() - 2))}
						onClose={() => setChromeOverflow(null)}
						onError={(error) => {
							props.showToast({
								title: "Status action failed",
								subtitle:
									error instanceof Error ? error.message : String(error),
								variant: "error",
							});
						}}
					/>
				)}
			</Show>

			{/* Composer picker only serves @/# references */}
			<CommandPalette picker={props.controller.commandPalette} />
			<Show when={props.overlays().length > 0}>
				<For each={props.overlays()}>
					{(entry, index) =>
						entry.component({
							done: (result: unknown) => entry.resolve(result),
							surfaceProps: getOverlaySurfaceProps(index()),
							get active() {
								return index() === props.overlays().length - 1;
							},
						})
					}
				</For>
			</Show>

			<ToastStack
				toasts={props.state.toasts}
				top={headerHeight()}
				zIndex={getToastStackZIndex(props.overlays().length)}
				onDismiss={props.dismissToast}
			/>
		</box>
	);
}

export function AppShell(props: AppShellProps) {
	const [settings, setSettings] = createSignal(props.settings);
	const reportKeybindingDiagnostic = createKeybindingDiagnosticReporter(
		props.showToast,
	);

	onCleanup(
		props.runtime.subscribe("settings.changed", (event) => {
			setSettings(event.settings);
		}),
	);

	function persistReviewDiffView(view: ReviewDiffView): void {
		void updateSettings((current) => ({
			...current,
			diffs: { view },
		}))
			.then((next) => props.runtime.emitSettingsChanged(next))
			.catch((error) => {
				props.showToast({
					title: "Failed to save diff view",
					subtitle: error instanceof Error ? error.message : String(error),
					variant: "error",
				});
			});
	}

	function persistPreferredPaneRatio(ratio: number): void {
		void updateSettings((current) => ({
			...current,
			workspace: { ...current.workspace, paneRatio: ratio },
		}))
			.then((next) => props.runtime.emitSettingsChanged(next))
			.catch((error) => {
				props.showToast({
					title: "Could not save workspace layout",
					subtitle: error instanceof Error ? error.message : String(error),
					variant: "error",
				});
			});
	}

	return (
		<KeymapLayerProvider
			keybindings={() => settings().keybindings}
			onDiagnostic={reportKeybindingDiagnostic}
		>
			<AppShellContent
				state={props.state}
				runtime={props.runtime}
				commands={props.commands}
				controller={props.controller}
				attachments={props.attachments}
				footer={props.footer}
				header={props.header}
				reviewDrafts={props.reviewDrafts}
				reviewWorkspace={props.reviewWorkspace}
				scratchpad={props.scratchpad}
				overlays={props.overlays}
				openOverlay={props.openOverlay}
				dismissToast={props.dismissToast}
				onTranscriptViewportChange={props.onTranscriptViewportChange}
				showToast={props.showToast}
				preferredPaneRatio={settings().workspace?.paneRatio}
				defaultReviewDiffView={resolveDiffSettings(settings().diffs).view}
				onReviewDiffViewChanged={persistReviewDiffView}
				onPreferredPaneRatioCommit={persistPreferredPaneRatio}
			/>
		</KeymapLayerProvider>
	);
}
