import type { Selection } from "@opentui/core";
import { useKeymapSelector } from "@opentui/keymap/solid";
import { useRenderer } from "@opentui/solid";
import { createEffect, createSignal, For, onCleanup, Show } from "solid-js";
import {
	getOverlaySurfaceProps,
	getToastStackZIndex,
	type OverlayEntry,
} from "../app/overlay-ui";
import type { Command, CommandRegistry } from "../features/commands";
import { registerMermaidPreviewHandler } from "../features/mermaid-preview/requests";
import type { ReleasesWorkspaceController } from "../features/releases";
import { CodeReviewAttachment } from "../features/review/attachment";
import type { ReviewDraftController } from "../features/review/draft-controller";
import type { ReviewWorkspaceController } from "../features/review/workspace-controller";
import type { ScratchpadController } from "../features/scratchpad/controller";
import type { SubagentsWorkspaceController } from "../features/subagents";
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
import { createPickerManager } from "../state/picker-manager";
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
import { copyToClipboard } from "./clipboard";
import type { ComposerController } from "./composer-controller";
import type { FooterStatusController } from "./footer-status";
import { HeaderBar } from "./HeaderBar";
import type { HeaderStatusController } from "./header-status";
import { InlinePicker } from "./InlinePicker";
import { formatCommandBindings } from "./KeymapHintBar";
import { PendingSlot } from "./PendingSlot";
import { SelectionContextMenu } from "./SelectionContextMenu";
import {
	applySelectionColors,
	formatSelectionAsQuote,
	restoreSelectionColors,
	type SelectionColorRestore,
} from "./selection";
import { ToastStack } from "./ToastStack";
import { theme } from "./theme";
import { Transcript } from "./transcript";
import type { ActivitySource } from "./transcript/turn-activity-view";
import type { OpenActivity, OpenOverlay } from "./transcript/types";
import { WorkspacePaneHost } from "./WorkspacePaneHost";
import { WorkspaceTabOverflowPicker } from "./WorkspaceTabOverflowPicker";
import {
	resolveWorkspacePaneLayout,
	WORKSPACE_MIN_PRIMARY_COLUMNS,
	WORKSPACE_MIN_SECONDARY_COLUMNS,
} from "./workspace-layout";
import {
	type WorkspacePane,
	type WorkspacePaneRenderContext,
	WorkspacePaneSurface,
	workspacePaneAvailable,
	workspacePaneClosable,
	workspacePaneIdentity,
	workspacePaneLabel,
	workspacePaneMinColumns,
} from "./workspace-pane-registry";
import {
	createWorkspaceStateController,
	DEFAULT_WORKSPACE_PANE_RATIO,
	type WorkspaceFocusedSurface,
	type WorkspaceState,
} from "./workspace-state";
import type { WorkspaceTabItem } from "./workspace-tabs-layout";

export type AppShellProps = {
	settings: Settings;
	state: AppState;
	runtime: AgentRuntime;
	commands: CommandRegistry;
	controller: ComposerController;
	attachments: AttachmentsController;
	footer: FooterStatusController;
	header: HeaderStatusController;
	releasesWorkspace: ReleasesWorkspaceController;
	reviewDrafts: ReviewDraftController;
	reviewWorkspace: ReviewWorkspaceController;
	scratchpad: ScratchpadController;
	subagentsWorkspace: SubagentsWorkspaceController;
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

export function activateExistingActivityTab(options: {
	tabId: string;
	source: ActivitySource;
	update: (pane: { kind: "activity"; source: ActivitySource }) => void;
	activate: (tabId: string) => void;
}): void {
	options.update({ kind: "activity", source: options.source });
	options.activate(options.tabId);
}

export function unavailableSubagentPaneTabIds(
	tabs: readonly { id: string; pane: WorkspacePane }[],
	conversationAgentNames: readonly string[] | null,
): string[] {
	const activeNames = new Set(conversationAgentNames ?? []);
	return tabs
		.filter(
			(tab) =>
				(tab.pane.kind === "subagents" && conversationAgentNames === null) ||
				(tab.pane.kind === "subagent" &&
					(conversationAgentNames === null ||
						!activeNames.has(tab.pane.agentName))),
		)
		.map((tab) => tab.id);
}

export function shouldFocusSubagentsRosterAfterRemoval(options: {
	focusedSurface: WorkspaceFocusedSurface;
	activeTabId: string | undefined;
	closingTabIds: readonly string[];
	rosterTabId: string | undefined;
}): boolean {
	return Boolean(
		options.focusedSurface === "secondary" &&
			options.activeTabId &&
			options.closingTabIds.includes(options.activeTabId) &&
			options.rosterTabId &&
			!options.closingTabIds.includes(options.rosterTabId),
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
	const [shellHeight, setShellHeight] = createSignal(renderer.terminalHeight);
	let shellRef:
		| { width: number; height: number; x: number; y: number }
		| undefined;
	const [selectionMenu, setSelectionMenu] = createSignal<{
		text: string;
		x: number;
		y: number;
		selection: Selection;
	} | null>(null);
	const selectionColorRestore: SelectionColorRestore = new Map();

	const workspace = createWorkspaceStateController<WorkspacePane>({
		preferredPaneRatio: props.preferredPaneRatio,
		identityOf: workspacePaneIdentity,
	});
	const workspaceOverflowPicker = createPickerManager();
	const [workspaceOverflowGuard, setWorkspaceOverflowGuard] = createSignal<{
		width: number;
		tabs: string;
	} | null>(null);
	const [workspaceState, setWorkspaceState] = createSignal<
		WorkspaceState<WorkspacePane>
	>(workspace.getState());
	onCleanup(workspace.subscribe(setWorkspaceState));
	const secondaryTabs = () => {
		const secondary = workspaceState().secondary;
		return secondary.status === "empty" ? [] : secondary.tabs;
	};
	const activeWorkspaceTab = () => {
		const secondary = workspaceState().secondary;
		if (secondary.status === "empty") return null;
		return (
			secondary.tabs.find((tab) => tab.id === secondary.activeTabId) ?? null
		);
	};
	const workspaceTabItems = (): WorkspaceTabItem[] => {
		const tabs = secondaryTabs();
		return tabs.map((tab) => ({
			id: tab.id,
			label: workspacePaneLabel(tab.pane, tabs),
			closable: workspacePaneClosable(tab.pane),
		}));
	};
	const focusedSurface = () => workspaceState().focusedSurface;
	const workspaceInteractionsEnabled = () =>
		focusedSurface() === "secondary" &&
		props.overlays().length === 0 &&
		chromeOverflow() === null &&
		selectionMenu() === null &&
		!workspaceOverflowPicker.visible &&
		!props.controller.picker.visible &&
		!props.controller.commandPalette.visible;
	function focusComposerSurface(): void {
		workspace.setNarrowTab("transcript");
		workspace.setFocusedSurface("composer");
	}
	function focusReviewSurface(): void {
		props.controller.picker.clear();
		workspace.setNarrowTab("secondary");
		workspace.setFocusedSurface("secondary");
	}
	function colorCurrentSelection(): void {
		if (props.overlays().length > 0) return;
		const selection = renderer.getSelection();
		if (!selection) return;
		applySelectionColors(
			selection,
			selectionColorRestore,
			theme.pickerFocusedBg,
			theme.pickerFocusedText,
		);
	}
	function discardSelection(): void {
		restoreSelectionColors(selectionColorRestore);
		renderer.clearSelection();
	}
	function closeSelectionMenu(): void {
		const restoreComposerFocus =
			focusedSurface() === "composer" && props.overlays().length === 0;
		setSelectionMenu(null);
		discardSelection();
		if (restoreComposerFocus) {
			queueMicrotask(() => props.controller.focusTextarea());
		}
	}
	function copySelectedText(): void {
		const selected = selectionMenu();
		if (!selected) return;
		closeSelectionMenu();
		void copyToClipboard(selected.text).catch((error) => {
			props.showToast({
				title: "Could not copy selection",
				subtitle: error instanceof Error ? error.message : String(error),
				variant: "error",
			});
		});
	}
	function quoteSelectedText(): void {
		const selected = selectionMenu();
		if (!selected) return;
		const composerText = props.controller.getTextareaText();
		const cursorOffset = props.controller.getTextareaCursorOffset();
		const quote = formatSelectionAsQuote(
			selected.text,
			cursorOffset > 0 && composerText[cursorOffset - 1] !== "\n",
		);
		closeSelectionMenu();
		if (!quote) return;
		focusComposerSurface();
		queueMicrotask(() => {
			props.controller.insertText(quote);
			props.controller.focusTextarea();
		});
	}
	onCleanup(discardSelection);
	onCleanup(
		props.reviewWorkspace.subscribe(() => {
			saveScratchpadDraftIfEditing();
			workspace.openSecondary({ kind: "review" }, { focus: "secondary" });
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
	createEffect(() => {
		if (!selectionMenu()) return;
		if (
			props.overlays().length > 0 ||
			chromeOverflow() !== null ||
			workspaceOverflowPicker.visible ||
			props.controller.picker.visible ||
			props.controller.commandPalette.visible
		) {
			closeSelectionMenu();
		}
	});

	function tabForKind(kind: WorkspacePane["kind"]) {
		return secondaryTabs().find((tab) => tab.pane.kind === kind) ?? null;
	}
	const activityTab = () => tabForKind("activity");
	const [subagentsData, setSubagentsData] = createSignal(
		props.subagentsWorkspace.data(),
	);
	const [subagentsRevision, setSubagentsRevision] = createSignal(0);
	onCleanup(
		props.subagentsWorkspace.subscribe(() =>
			setSubagentsData(props.subagentsWorkspace.data()),
		),
	);
	createEffect(() => {
		const data = subagentsData();
		if (!data) return;
		onCleanup(
			data.subscribeToChanges(() =>
				setSubagentsRevision((revision) => revision + 1),
			),
		);
	});
	const secondaryPaneVisible = () => {
		if (workspaceState().secondary.status !== "open") return false;
		const tab = activeWorkspaceTab();
		return tab
			? workspacePaneAvailable(tab.pane, workspacePaneContext(tab.id))
			: false;
	};
	const secondaryPaneMinColumns = () => {
		const pane = activeWorkspaceTab()?.pane;
		return pane
			? workspacePaneMinColumns(pane)
			: WORKSPACE_MIN_SECONDARY_COLUMNS;
	};
	const supportsNarrowWorkspaceTabs = () => secondaryTabs().length > 0;
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

	createEffect(() => {
		if (!workspaceOverflowPicker.visible) return;
		const guard = workspaceOverflowGuard();
		const tabSignature = secondaryTabs()
			.map((tab) => tab.id)
			.join("\u0000");
		if (
			!guard ||
			!workspaceUsesNarrowTabs() ||
			guard.width !== shellWidth() ||
			guard.tabs !== tabSignature
		) {
			workspaceOverflowPicker.clear();
			setWorkspaceOverflowGuard(null);
		}
	});

	let workspaceSessionId = props.runtime.getSession().id;
	onCleanup(
		props.runtime.subscribe("session.active.changed", (event) => {
			if (event.session.id === workspaceSessionId) return;
			workspaceSessionId = event.session.id;
			saveScratchpadDraftIfEditing();
			workspaceOverflowPicker.clear();
			setWorkspaceOverflowGuard(null);
			workspace.clearSecondary();
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

	function workspacePaneContext(tabId: string): WorkspacePaneRenderContext {
		return {
			active: () =>
				activeWorkspaceTab()?.id === tabId && workspaceInteractionsEnabled(),
			onFocusRequest: () => {
				focusSecondarySurface(tabId);
			},
			onLeave: leaveWorkspaceSurface,
			runtime: props.runtime,
			attachments: props.attachments,
			reviewDrafts: props.reviewDrafts,
			defaultReviewDiffView: () => props.defaultReviewDiffView,
			onReviewDiffViewChanged: props.onReviewDiffViewChanged,
			onSubmitReviewMessage: () => props.controller.handleMessageSubmit(),
			releasesWorkspace: props.releasesWorkspace,
			scratchpad: props.scratchpad,
			subagentsData,
			openSubagents: openSubagentsPanel,
			openSubagent: openSubagentPanel,
			closePane: () => {
				requestCloseTab(tabId);
			},
			openOverlay: props.openOverlay,
			showToast: props.showToast,
		};
	}

	function focusSecondarySurface(tabId?: string): boolean {
		const target = tabId ?? activeWorkspaceTab()?.id;
		if (!target) return false;
		if (
			activeWorkspaceTab()?.pane.kind === "scratchpad" &&
			activeWorkspaceTab()?.id !== target
		) {
			saveScratchpadDraftIfEditing();
		}
		props.controller.picker.clear();
		workspace.selectSecondary(target, { focus: "secondary" });
		return true;
	}

	function requestCloseTab(tabId: string): boolean {
		const tab = secondaryTabs().find((candidate) => candidate.id === tabId);
		if (!tab) return false;
		if (!workspacePaneClosable(tab.pane)) return false;
		workspace.closeSecondary(tabId);
		if (workspaceState().secondary.status === "empty") focusComposerSurface();
		return true;
	}

	const openActivity: OpenActivity = (source) => {
		const existing = activityTab();
		if (existing) {
			activateExistingActivityTab({
				tabId: existing.id,
				source,
				update: (pane) => {
					workspace.updateSecondary(pane);
				},
				activate: focusSecondarySurface,
			});
			return;
		}
		saveScratchpadDraftIfEditing();
		workspace.openSecondary(
			{ kind: "activity", source },
			{ focus: "secondary" },
		);
		focusSecondarySurface();
	};

	function openMermaidPreview(source: string): void {
		saveScratchpadDraftIfEditing();
		workspace.openSecondary(
			{ kind: "mermaid", source },
			{ focus: "secondary" },
		);
		focusSecondarySurface();
	}

	onCleanup(registerMermaidPreviewHandler(openMermaidPreview));

	function leaveWorkspaceSurface(): void {
		saveScratchpadDraftIfEditing();
		focusComposerSurface();
	}

	function openReleasesPanel(): void {
		saveScratchpadDraftIfEditing();
		workspace.openSecondary({ kind: "releases" }, { focus: "secondary" });
		focusSecondarySurface();
	}

	onCleanup(props.releasesWorkspace.onOpenRequest(openReleasesPanel));

	const toggleScratchpad = () => {
		const tab = tabForKind("scratchpad");
		if (
			tab &&
			activeWorkspaceTab()?.id === tab.id &&
			workspaceState().secondary.status === "open"
		) {
			saveScratchpadDraftIfEditing();
			workspace.minimizeSecondary();
			focusComposerSurface();
			return;
		}
		saveScratchpadDraftIfEditing();
		workspace.openSecondary({ kind: "scratchpad" }, { focus: "secondary" });
		focusSecondarySurface();
	};

	function openSubagentsPanel(): void {
		if (subagentsData() === null) return;
		saveScratchpadDraftIfEditing();
		workspace.openSecondary({ kind: "subagents" }, { focus: "secondary" });
		focusSecondarySurface();
	}

	function openSubagentPanel(agentName: string): void {
		const data = subagentsData();
		if (
			!data
				?.getActiveConversations()
				.some((conversation) => conversation.agentName === agentName)
		) {
			return;
		}
		saveScratchpadDraftIfEditing();
		workspace.openSecondary(
			{ kind: "subagent", agentName },
			{ focus: "secondary" },
		);
		focusSecondarySurface();
	}

	onCleanup(props.subagentsWorkspace.onOpenRequest(openSubagentsPanel));

	createEffect(() => {
		void subagentsRevision();
		const data = subagentsData();
		const conversationAgentNames =
			data
				?.getActiveConversations()
				.map((conversation) => conversation.agentName) ?? null;
		const tabs = secondaryTabs();
		const tabIds = unavailableSubagentPaneTabIds(tabs, conversationAgentNames);
		const activeTabId = activeWorkspaceTab()?.id;
		const rosterTabId = tabs.find((tab) => tab.pane.kind === "subagents")?.id;
		const focusRoster = shouldFocusSubagentsRosterAfterRemoval({
			focusedSurface: focusedSurface(),
			activeTabId,
			closingTabIds: tabIds,
			rosterTabId,
		});
		for (const tabId of tabIds) workspace.closeSecondary(tabId);
		if (focusRoster && rosterTabId) focusSecondarySurface(rosterTabId);
	});

	function cycleWorkspaceFocus(direction: -1 | 1): boolean {
		if (props.scratchpad.editing()) props.scratchpad.autosaveDraft();
		return workspace.cycleSurface(direction);
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

	function toggleSecondaryPane(): boolean {
		const secondary = workspaceState().secondary;
		if (secondary.status === "empty") return false;
		if (secondary.status === "open") {
			if (activeWorkspaceTab()?.pane.kind === "scratchpad") {
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

	function openWorkspaceOverflow(tabs: readonly WorkspaceTabItem[]): void {
		if (tabs.length === 0) return;
		setWorkspaceOverflowGuard({
			width: shellWidth(),
			tabs: secondaryTabs()
				.map((tab) => tab.id)
				.join("\u0000"),
		});
		workspaceOverflowPicker.show({
			label: "Workspace surfaces",
			filterable: true,
			options: tabs.map((tab) => ({
				name: tab.label,
				description: "",
				value: tab.id,
				action: (context) => {
					context.dismiss();
					focusSecondarySurface(tab.id);
				},
			})),
		});
	}

	const workspaceCommandHandlers = {
		"workspace.focus-next": () => cycleWorkspaceFocus(1),
		"workspace.focus-previous": () => cycleWorkspaceFocus(-1),
		"workspace.focus-primary": () => {
			focusComposerSurface();
			return true;
		},
		"workspace.focus-secondary": () => focusSecondarySurface(),
		"workspace.close-tab": () => {
			const tab = activeWorkspaceTab();
			return tab ? requestCloseTab(tab.id) : false;
		},
		"workspace.toggle-secondary": toggleSecondaryPane,
		"workspace.reset-layout": resetWorkspaceLayout,
	} as const;
	const workspaceKeymapHandlers = Object.fromEntries(
		Object.entries(workspaceCommandHandlers).map(([name, execute]) => [
			name,
			() => {
				if (props.controller.picker.visible) return false;
				if (workspaceOverflowPicker.visible) return false;
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

	const chromeActionsDisabled = () =>
		props.overlays().length > 0 ||
		chromeOverflow() !== null ||
		selectionMenu() !== null ||
		props.controller.picker.visible ||
		workspaceOverflowPicker.visible ||
		props.controller.commandPalette.visible;

	function runHeaderCommand(name: string): void {
		if (props.overlays().length > 0 || chromeOverflow() || selectionMenu())
			return;
		if (props.controller.picker.visible) return;
		if (props.controller.commandPalette.visible) return;
		const command = props.commands
			.getAll()
			.find((candidate) => candidate.name === name);
		if (command) void props.controller.runCommand(command, "");
	}

	useKeymapLayer(() => {
		commandRegistryVersion();
		const bindableCommands = props.commands
			.getAll()
			.filter((command) => !getKeybindingCommand(command.name));
		return {
			scope: "app",
			when: () =>
				props.overlays().length === 0 &&
				chromeOverflow() === null &&
				selectionMenu() === null &&
				!workspaceOverflowPicker.visible,
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
				const openSelection = selectionMenu();
				if (openSelection) {
					restoreSelectionColors(selectionColorRestore);
					setSelectionMenu(null);
					if (renderer.getSelection() === openSelection.selection) {
						renderer.clearSelection();
					}
				}
				if (chromeOverflow()) setChromeOverflow(null);
				if (workspaceOverflowPicker.visible) workspaceOverflowPicker.clear();
				colorCurrentSelection();
			}}
			onMouseMove={() => colorCurrentSelection()}
			onMouseDrag={() => colorCurrentSelection()}
			onMouseUp={(event) => {
				if (event.button !== 0) return;
				if (
					props.overlays().length > 0 ||
					chromeOverflow() ||
					workspaceOverflowPicker.visible ||
					props.controller.picker.visible ||
					props.controller.commandPalette.visible
				) {
					discardSelection();
					return;
				}
				colorCurrentSelection();
				const selection = renderer.getSelection();
				const text = selection?.getSelectedText();
				if (!selection || !text) {
					discardSelection();
					return;
				}
				setSelectionMenu({
					text,
					x: event.x - (shellRef?.x ?? 0),
					y: event.y - (shellRef?.y ?? 0),
					selection,
				});
			}}
			ref={(value) => {
				shellRef = value as typeof shellRef;
			}}
			onSizeChange={() => {
				if (!shellRef) return;
				if (
					selectionMenu() &&
					(shellRef.width !== shellWidth() || shellRef.height !== shellHeight())
				) {
					closeSelectionMenu();
				}
				setShellWidth(shellRef.width);
				setShellHeight(shellRef.height);
			}}
		>
			<HeaderBar
				runtime={props.runtime}
				header={props.header}
				sessionName={props.state.sessionMeta.name}
				shellWidth={shellWidth()}
				transcriptWidth={transcriptWidth()}
				actionsDisabled={chromeActionsDisabled}
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
				onRenameSession={() => runHeaderCommand("name")}
				onSelectModel={() => runHeaderCommand("model")}
				onSelectThinkingLevel={() => runHeaderCommand("thinking")}
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
				tabs={workspaceTabItems}
				activeTabId={() => activeWorkspaceTab()?.id ?? ""}
				selectedSurface={() =>
					workspaceState().narrowTab === "transcript"
						? "transcript"
						: (activeWorkspaceTab()?.id ?? "transcript")
				}
				drawerCollapsed={() =>
					workspaceState().secondary.status === "minimized"
				}
				initialWidth={Math.max(1, shellWidth() - 2)}
				preferredPaneRatio={() => workspaceState().preferredPaneRatio}
				minPrimaryColumns={WORKSPACE_MIN_PRIMARY_COLUMNS}
				minSecondaryColumns={() =>
					Math.max(WORKSPACE_MIN_SECONDARY_COLUMNS, secondaryPaneMinColumns())
				}
				onPreferredPaneRatioChange={(ratio) =>
					workspace.setPreferredPaneRatio(ratio)
				}
				onPreferredPaneRatioCommit={props.onPreferredPaneRatioCommit}
				onDividerMouseDown={() => renderer.clearSelection()}
				onSelectTranscript={focusComposerSurface}
				onSelectTab={(tabId) => focusSecondarySurface(tabId)}
				onCloseTab={requestCloseTab}
				onCollapseDrawer={() => {
					saveScratchpadDraftIfEditing();
					workspace.minimizeSecondary();
					focusComposerSurface();
				}}
				onExpandDrawer={() => {
					if (workspaceState().secondary.status === "empty") {
						toggleScratchpad();
						return;
					}
					workspace.restoreSecondary();
				}}
				onOpenOverflow={openWorkspaceOverflow}
				secondary={() => (
					<For each={secondaryTabs()}>
						{(tab) => (
							<WorkspacePaneSurface
								pane={tab.pane}
								selected={() => activeWorkspaceTab()?.id === tab.id}
								context={workspacePaneContext(tab.id)}
							/>
						)}
					</For>
				)}
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
							locked={
								props.overlays().length > 0 ||
								chromeOverflow() !== null ||
								selectionMenu() !== null ||
								workspaceOverflowPicker.visible
							}
							inputFocused={
								chromeOverflow() === null &&
								!workspaceOverflowPicker.visible &&
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
				actionsDisabled={chromeActionsDisabled}
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

			<Show when={selectionMenu()}>
				{(menu) => (
					<SelectionContextMenu
						x={menu().x}
						y={menu().y}
						containerWidth={shellWidth()}
						containerHeight={shellHeight()}
						onCopy={copySelectedText}
						onQuote={quoteSelectedText}
						onClose={closeSelectionMenu}
					/>
				)}
			</Show>

			<Show when={workspaceOverflowPicker.visible}>
				<WorkspaceTabOverflowPicker
					picker={workspaceOverflowPicker}
					width={Math.max(1, Math.min(56, shellWidth() - 2))}
				/>
			</Show>

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
				releasesWorkspace={props.releasesWorkspace}
				reviewDrafts={props.reviewDrafts}
				reviewWorkspace={props.reviewWorkspace}
				scratchpad={props.scratchpad}
				subagentsWorkspace={props.subagentsWorkspace}
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
