import { useKeymapSelector } from "@opentui/keymap/solid";
import { useRenderer } from "@opentui/solid";
import { createEffect, createSignal, For, onCleanup, Show } from "solid-js";
import {
	getOverlaySurfaceProps,
	getToastStackZIndex,
	type OverlayEntry,
} from "../app/overlay-ui";
import type { Command, CommandRegistry } from "../features/commands";
import { CodeReviewAttachment } from "../features/review/attachment";
import type { ReviewDraftController } from "../features/review/draft-controller";
import { ReviewContent } from "../features/review/ReviewContent";
import type { ReviewWorkspaceController } from "../features/review/workspace-controller";
import type { ScratchpadController } from "../features/scratchpad/controller";
import {
	SCRATCHPAD_MIN_COLS,
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
import { formatCommandBindings } from "./KeymapHintBar";
import { PendingSlot } from "./PendingSlot";
import { copySelection } from "./selection";
import { ToastStack } from "./ToastStack";
import { theme } from "./theme";
import { Transcript } from "./transcript";
import { TurnActivityPanel } from "./transcript/TurnActivityPanel";
import type { ActivitySource } from "./transcript/turn-activity-view";
import type { OpenActivity, OpenOverlay } from "./transcript/types";
import { WorkspacePaneHost } from "./WorkspacePaneHost";
import {
	resizeWorkspacePaneRatio,
	resolveWorkspacePaneLayout,
	WORKSPACE_MIN_PRIMARY_COLUMNS,
	WORKSPACE_MIN_SECONDARY_COLUMNS,
} from "./workspace-layout";
import {
	createWorkspaceStateController,
	DEFAULT_WORKSPACE_PANE_RATIO,
	type WorkspaceFocusedSurface,
	type WorkspaceState,
} from "./workspace-state";

const ACTIVITY_MIN_COLS = 40;
const REVIEW_MIN_COLS = 60;

type WorkspacePane =
	| { kind: "activity"; source: ActivitySource }
	| { kind: "review" }
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

	// Track outer terminal width for responsive workspace presentation.
	const [shellWidth, setShellWidth] = createSignal(renderer.terminalWidth);
	let shellRef: { width: number; height: number } | undefined;

	const workspace = createWorkspaceStateController<WorkspacePane>({
		preferredPaneRatio: props.preferredPaneRatio,
	});
	const [workspaceState, setWorkspaceState] = createSignal<
		WorkspaceState<WorkspacePane>
	>(workspace.getState());
	onCleanup(workspace.subscribe(setWorkspaceState));
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
		workspace.setNarrowTab("secondary");
		workspace.setFocusedSurface("secondary");
	}
	onCleanup(
		props.reviewWorkspace.subscribe(() => {
			saveScratchpadDraftIfEditing();
			workspace.openSecondary({ kind: "review" });
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

	const activitySource = () => {
		const panel = openSecondaryPane();
		return panel?.kind === "activity" ? panel.source : null;
	};
	const isEditableReviewPane = (pane: WorkspacePane | null | undefined) =>
		pane?.kind === "review";
	const editableReviewOpen = () => isEditableReviewPane(openSecondaryPane());
	const editableReviewRetained = () => {
		const secondary = workspaceState().secondary;
		if (secondary.status === "empty") return false;
		return (
			isEditableReviewPane(secondary.pane) ||
			isEditableReviewPane(secondary.returnPane)
		);
	};
	const isScratchpadPane = (pane: WorkspacePane | null | undefined) =>
		pane?.kind === "scratchpad";
	const scratchpadOpen = () => isScratchpadPane(openSecondaryPane());
	const scratchpadRetained = () => {
		const secondary = workspaceState().secondary;
		if (secondary.status === "empty") return false;
		return (
			isScratchpadPane(secondary.pane) || isScratchpadPane(secondary.returnPane)
		);
	};
	const secondaryPaneVisible = () =>
		editableReviewOpen() || activitySource() !== null || scratchpadOpen();
	const secondaryPaneMinColumns = () => {
		const pane = openSecondaryPane();
		if (pane?.kind === "scratchpad") return SCRATCHPAD_MIN_COLS;
		if (pane?.kind === "review") return REVIEW_MIN_COLS;
		return ACTIVITY_MIN_COLS;
	};
	const supportsNarrowWorkspaceTabs = () =>
		editableReviewOpen() || activitySource() !== null || scratchpadOpen();
	const workspaceUsesNarrowTabs = () =>
		supportsNarrowWorkspaceTabs() &&
		resolveWorkspacePaneLayout({
			availableColumns: Math.max(0, shellWidth() - 2),
			preferredPaneRatio: workspaceState().preferredPaneRatio,
			minPrimaryColumns: WORKSPACE_MIN_PRIMARY_COLUMNS,
			minSecondaryColumns: Math.max(
				WORKSPACE_MIN_SECONDARY_COLUMNS,
				secondaryPaneMinColumns(),
			),
		}) === null;
	const secondaryPaneLabel = () => {
		if (activitySource() !== null) return "Activity";
		if (scratchpadOpen()) return "Scratchpad";
		return "Code review";
	};

	onCleanup(
		props.runtime.subscribe("session.active.changed", () => {
			const state = workspaceState();
			const secondary = state.secondary;
			if (secondary.status === "empty") return;
			const scratchpad =
				secondary.pane.kind === "scratchpad"
					? secondary.pane
					: secondary.returnPane?.kind === "scratchpad"
						? secondary.returnPane
						: null;
			if (scratchpad) {
				if (secondary.status === "open") {
					workspace.openSecondary(scratchpad, {
						focus: state.focusedSurface,
					});
				} else {
					workspace.setActiveSecondary(scratchpad);
				}
				return;
			}
			if (
				secondary.pane.kind === "review" ||
				secondary.pane.kind === "activity"
			) {
				workspace.clearSecondary();
			}
		}),
	);

	const restoreQueueBinding = useKeymapSelector<string | undefined>(
		(keymap) => {
			const entry = keymap
				.getCommandEntries({
					visibility: "active",
					filter: (command) => command.name === "composer.restore-or-recall",
				})
				.at(0);
			return entry?.bindings.length
				? formatCommandBindings(entry.bindings)
				: undefined;
		},
	);

	function saveScratchpadDraftIfEditing(): void {
		if (props.scratchpad.editing()) props.scratchpad.autosaveDraft();
	}

	const openActivity: OpenActivity = (source) => {
		const current = openSecondaryPane();
		if (
			current?.kind === "activity" &&
			activitySourceEquals(current.source, source)
		) {
			focusSecondarySurface();
			return;
		}
		saveScratchpadDraftIfEditing();
		if (current?.kind === "activity") {
			const secondary = workspaceState().secondary;
			if (secondary.status === "open" && secondary.returnPane) {
				workspace.replaceSecondary(
					{ kind: "activity", source },
					{ focus: "secondary" },
				);
			} else {
				workspace.openSecondary(
					{ kind: "activity", source },
					{ focus: "secondary" },
				);
			}
		} else if (current) {
			workspace.pushSecondary(
				{ kind: "activity", source },
				{ focus: "secondary" },
			);
		} else {
			workspace.openSecondary(
				{ kind: "activity", source },
				{ focus: "secondary" },
			);
		}
		focusSecondarySurface();
	};

	function closeTemporaryPanel(): void {
		if (!workspace.popSecondary({ focus: "secondary" })) {
			workspace.minimizeSecondary();
			focusComposerSurface();
			return;
		}

		if (!focusSecondarySurface()) focusComposerSurface();
	}

	function closeActivityPanel(): void {
		closeTemporaryPanel();
	}

	function focusSecondarySurface(): boolean {
		if (!secondaryPaneVisible()) return false;
		const pane = openSecondaryPane();
		if (!pane) return false;
		if (pane.kind === "review") {
			focusReviewSurface();
		} else {
			props.controller.picker.clear();
			workspace.setNarrowTab("secondary");
			workspace.setFocusedSurface("secondary");
		}
		return true;
	}

	function cycleWorkspaceFocus(): boolean {
		if (!secondaryPaneVisible()) return false;
		if (focusedSurface() === "secondary") {
			focusComposerSurface();
			return true;
		}
		return focusSecondarySurface();
	}

	function resizeSecondaryPane(
		direction: "grow-secondary" | "shrink-secondary",
	): boolean {
		if (!secondaryPaneVisible()) return false;
		const ratio = resizeWorkspacePaneRatio({
			availableColumns: Math.max(0, shellWidth() - 2),
			preferredPaneRatio: workspaceState().preferredPaneRatio,
			minPrimaryColumns: WORKSPACE_MIN_PRIMARY_COLUMNS,
			minSecondaryColumns: Math.max(
				WORKSPACE_MIN_SECONDARY_COLUMNS,
				secondaryPaneMinColumns(),
			),
			direction,
		});
		if (ratio === null) return false;
		workspace.setPreferredPaneRatio(ratio);
		props.onPreferredPaneRatioCommit(ratio);
		return true;
	}

	function resetWorkspaceLayout(): boolean {
		if (!secondaryPaneVisible()) return false;
		const canSplit = resolveWorkspacePaneLayout({
			availableColumns: Math.max(0, shellWidth() - 2),
			preferredPaneRatio: workspaceState().preferredPaneRatio,
			minPrimaryColumns: WORKSPACE_MIN_PRIMARY_COLUMNS,
			minSecondaryColumns: Math.max(
				WORKSPACE_MIN_SECONDARY_COLUMNS,
				secondaryPaneMinColumns(),
			),
		});
		if (!canSplit) return false;
		workspace.setPreferredPaneRatio(DEFAULT_WORKSPACE_PANE_RATIO);
		props.onPreferredPaneRatioCommit(DEFAULT_WORKSPACE_PANE_RATIO);
		return true;
	}

	function closeScratchpadPanel(): void {
		saveScratchpadDraftIfEditing();
		closeTemporaryPanel();
	}

	const toggleScratchpad = () => {
		const secondary = workspaceState().secondary;
		const panel = openSecondaryPane();
		if (panel?.kind === "scratchpad") {
			closeScratchpadPanel();
			return;
		}
		if (
			secondary.status === "open" &&
			secondary.returnPane?.kind === "scratchpad"
		) {
			workspace.popSecondary({ focus: "secondary" });
			focusSecondarySurface();
			return;
		}
		if (
			secondary.status === "minimized" &&
			secondary.pane.kind === "scratchpad"
		) {
			workspace.restoreSecondary({ focus: "secondary" });
			focusSecondarySurface();
			return;
		}
		if (panel) {
			workspace.pushSecondary({ kind: "scratchpad" }, { focus: "secondary" });
		} else {
			workspace.openSecondary({ kind: "scratchpad" }, { focus: "secondary" });
		}
		focusSecondarySurface();
	};

	function toggleSecondaryPane(): boolean {
		const secondary = workspaceState().secondary;
		if (secondary.status === "empty") return false;
		if (secondary.status === "open") {
			if (secondary.pane.kind === "scratchpad") {
				saveScratchpadDraftIfEditing();
			}
			workspace.minimizeSecondary();
			focusComposerSurface();
			return true;
		}

		workspace.restoreSecondary({ focus: "secondary" });
		focusSecondarySurface();
		return true;
	}

	const workspaceCommandHandlers = {
		"workspace.focus-next": cycleWorkspaceFocus,
		"workspace.focus-previous": cycleWorkspaceFocus,
		"workspace.focus-primary": () => {
			focusComposerSurface();
			return true;
		},
		"workspace.focus-secondary": focusSecondarySurface,
		"workspace.toggle-secondary": toggleSecondaryPane,
		"workspace.grow-secondary": () => resizeSecondaryPane("grow-secondary"),
		"workspace.shrink-secondary": () => resizeSecondaryPane("shrink-secondary"),
		"workspace.reset-layout": resetWorkspaceLayout,
	} as const;
	const workspaceKeymapHandlers = Object.fromEntries(
		Object.entries(workspaceCommandHandlers).map(([name, execute]) => [
			name,
			() => {
				if (props.controller.picker.visible) return false;
				if (props.controller.commandPalette.visible) return false;
				return execute();
			},
		]),
	) as typeof workspaceCommandHandlers;

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

	const disposeWorkspaceCommands = Object.entries(workspaceCommandHandlers).map(
		([name, execute]) =>
			props.commands.register({
				name,
				description:
					getKeybindingCommand(name)?.desc ?? "Control workspace layout",
				category: "App",
				execute: () => {
					execute();
				},
			}),
	);
	onCleanup(() => {
		for (const dispose of disposeWorkspaceCommands) dispose();
	});

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
				...workspaceKeymapHandlers,
				"command-palette.open": () => {
					props.controller.openCommandPalette();
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
					!workspaceUsesNarrowTabs() ||
					workspaceState().narrowTab === "transcript"
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
			 * activity panel is open it mounts to the right and extends
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
					supportsNarrowWorkspaceTabs()
						? {
								selected: () => workspaceState().narrowTab,
								secondaryLabel: secondaryPaneLabel,
								onSelect: (tab) => {
									if (tab === "secondary") focusSecondarySurface();
									else focusComposerSurface();
								},
							}
						: undefined
				}
				secondary={
					<>
						<Show when={editableReviewRetained()}>
							<box
								position="absolute"
								left={editableReviewOpen() ? 0 : -1000}
								top={0}
								width="100%"
								height="100%"
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
									onFocusRequest={focusReviewSurface}
									active={
										editableReviewOpen() &&
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
						<Show keyed when={activitySource()}>
							{(source) => (
								<box
									position="absolute"
									left={0}
									top={0}
									width="100%"
									height="100%"
									onMouseDown={(event) => {
										if (event.button === 0) focusSecondarySurface();
									}}
								>
									<TurnActivityPanel
										runtime={props.runtime}
										source={source}
										active={
											focusedSurface() === "secondary" &&
											props.overlays().length === 0 &&
											chromeOverflow() === null &&
											!props.controller.picker.visible &&
											!props.controller.commandPalette.visible
										}
										onClose={closeActivityPanel}
										onFocusRequest={focusSecondarySurface}
									/>
								</box>
							)}
						</Show>
						<Show when={scratchpadRetained()}>
							<box
								position="absolute"
								left={scratchpadOpen() ? 0 : -1000}
								top={0}
								width="100%"
								height="100%"
							>
								<ScratchpadPanel
									controller={props.scratchpad}
									active={
										scratchpadOpen() &&
										props.overlays().length === 0 &&
										chromeOverflow() === null &&
										!props.controller.picker.visible &&
										!props.controller.commandPalette.visible &&
										focusedSurface() === "secondary"
									}
									onClose={closeScratchpadPanel}
									onFocusRequest={focusSecondarySurface}
								/>
							</box>
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
									props.reviewWorkspace.open();
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
				restoreQueueBinding={restoreQueueBinding()}
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
