import path from "node:path";
import { type JSX, Show } from "solid-js";
import { FileViewerPanel } from "../features/files";
import type { FileCommentDraftController } from "../features/files/comment-draft-controller";
import { type McpPanelData, McpStatusPanel } from "../features/mcp";
import { MermaidPreviewPanel } from "../features/mermaid-preview/MermaidPreviewPanel";
import {
	ReleaseNotesPanel,
	type ReleasesWorkspaceController,
} from "../features/releases";
import type { ReviewDraftController } from "../features/review/draft-controller";
import { ReviewContent } from "../features/review/ReviewContent";
import type { ScratchpadController } from "../features/scratchpad/controller";
import { ScratchpadPanel } from "../features/scratchpad/ScratchpadPanel";
import type { SubagentsPanelData } from "../features/subagents";
import { SubagentPanel } from "../features/subagents/SubagentPanel";
import { SubagentsPanel } from "../features/subagents/SubagentsPanel";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { ReviewDiffView } from "../settings";
import type { ToastInput } from "../state/toasts";
import type { AttachmentsController } from "./attachments-controller";
import { openExternal } from "./open-external";
import { TurnActivityPanel } from "./transcript/TurnActivityPanel";
import type { ActivitySource } from "./transcript/turn-activity-view";
import type { OpenOverlay } from "./transcript/types";

export type WorkspacePane =
	| { kind: "activity"; source: ActivitySource }
	| { kind: "file"; repoRoot: string; path: string }
	| { kind: "mermaid"; source: string }
	| { kind: "mcp" }
	| { kind: "review" }
	| { kind: "releases" }
	| { kind: "scratchpad" }
	| { kind: "subagents" }
	| { kind: "subagent"; agentName: string };

export const DEFAULT_WORKSPACE_PANES = [
	{ kind: "review" },
	{ kind: "scratchpad" },
] as const satisfies readonly WorkspacePane[];

export type WorkspacePaneKind = WorkspacePane["kind"];
type PaneOfKind<K extends WorkspacePaneKind> = Extract<
	WorkspacePane,
	{ kind: K }
>;

export type WorkspacePaneRenderContext = {
	active: () => boolean;
	onFocusRequest: () => void;
	onLeave: () => void;
	runtime: AgentRuntime;
	mcpData: () => McpPanelData | null;
	attachments: AttachmentsController;
	fileCommentDrafts: FileCommentDraftController;
	reviewDrafts: ReviewDraftController;
	reviewRepoRoot: () => string;
	defaultReviewDiffView: () => ReviewDiffView;
	onReviewDiffViewChanged: (view: ReviewDiffView) => void;
	onSubmitReviewMessage: () => void | Promise<void>;
	releasesWorkspace: ReleasesWorkspaceController;
	scratchpad: ScratchpadController;
	subagentsData: () => SubagentsPanelData | null;
	openFile: (path: string) => void;
	openFileFinder: () => void;
	openSubagents: () => void;
	openSubagent: (agentName: string) => boolean;
	closePane: () => void;
	openOverlay: OpenOverlay;
	showToast: (toast: ToastInput) => void;
};

export type WorkspacePaneDefinition<K extends WorkspacePaneKind> = {
	kind: K;
	identity: (pane: PaneOfKind<K>) => string;
	label: (
		pane: PaneOfKind<K>,
		tabs: readonly { pane: WorkspacePane }[],
	) => string;
	closable: boolean;
	available?: (context: WorkspacePaneRenderContext) => boolean;
	render: (
		pane: PaneOfKind<K>,
		context: WorkspacePaneRenderContext,
	) => JSX.Element;
};

type WorkspacePaneDefinitionMap = {
	[K in WorkspacePaneKind]: WorkspacePaneDefinition<K>;
};

function filePaneLabel(
	pane: PaneOfKind<"file">,
	tabs: readonly { pane: WorkspacePane }[],
): string {
	const basename = path.basename(pane.path) || pane.path;
	const duplicate = tabs.some(
		(tab) =>
			tab.pane.kind === "file" &&
			tab.pane.path !== pane.path &&
			path.basename(tab.pane.path) === basename,
	);
	return duplicate ? pane.path : basename;
}

export const WORKSPACE_PANE_DEFINITIONS = {
	activity: {
		kind: "activity",
		identity: () => "activity",
		label: () => "Activity",
		closable: true,
		render: (pane, context) => (
			<TurnActivityPanel
				runtime={context.runtime}
				source={pane.source}
				active={context.active()}
				onClose={context.onLeave}
				onFocusRequest={context.onFocusRequest}
				onOpenSubagent={context.openSubagent}
			/>
		),
	},
	file: {
		kind: "file",
		identity: (pane) => `file:${path.resolve(pane.repoRoot, pane.path)}`,
		label: filePaneLabel,
		closable: true,
		render: (pane, context) => (
			<FileViewerPanel
				repoRoot={pane.repoRoot}
				path={pane.path}
				active={context.active()}
				onOpenFile={context.openFile}
				onClose={context.closePane}
				attachments={context.attachments}
				commentDrafts={context.fileCommentDrafts}
				toast={context.showToast}
				onSubmitMessage={context.onSubmitReviewMessage}
				onFocusRequest={context.onFocusRequest}
			/>
		),
	},
	mermaid: {
		kind: "mermaid",
		identity: (pane) => `mermaid:${pane.source}`,
		label: (pane, tabs) => {
			const diagrams = tabs.filter((tab) => tab.pane.kind === "mermaid");
			const index = diagrams.findIndex(
				(tab) => tab.pane.kind === "mermaid" && tab.pane.source === pane.source,
			);
			return diagrams.length > 1 ? `Diagram ${index + 1}` : "Diagram";
		},
		closable: true,
		render: (pane, context) => (
			<MermaidPreviewPanel
				source={pane.source}
				active={context.active()}
				onClose={context.onLeave}
				onFocusRequest={context.onFocusRequest}
				onActionError={(error) =>
					context.showToast({
						title: "Could not open diagram",
						subtitle: error instanceof Error ? error.message : String(error),
						variant: "error",
					})
				}
			/>
		),
	},
	mcp: {
		kind: "mcp",
		identity: () => "mcp",
		label: () => "MCP",
		closable: true,
		available: (context) => context.mcpData() !== null,
		render: (_pane, context) => (
			<McpStatusPanel
				data={context.mcpData}
				active={context.active()}
				onClose={context.onLeave}
				onFocusRequest={context.onFocusRequest}
			/>
		),
	},
	review: {
		kind: "review",
		identity: () => "review",
		label: () => "Code review",
		closable: false,
		render: (_pane, context) => (
			<Show keyed when={context.reviewRepoRoot()}>
				{(repoRoot) => (
					<ReviewContent
						repoRoot={repoRoot}
						onClose={context.onLeave}
						onTabClose={() => {}}
						attachments={context.attachments}
						reviewDrafts={context.reviewDrafts}
						toast={context.showToast}
						defaultDiffView={context.defaultReviewDiffView()}
						onDiffViewChanged={context.onReviewDiffViewChanged}
						onFocusRequest={context.onFocusRequest}
						onSubmitMessage={context.onSubmitReviewMessage}
						active={context.active()}
						onOpenFile={context.openFile}
						onFindFile={context.openFileFinder}
					/>
				)}
			</Show>
		),
	},
	releases: {
		kind: "releases",
		identity: () => "releases",
		label: () => "Release notes",
		closable: true,
		render: (_pane, context) => (
			<ReleaseNotesPanel
				controller={context.releasesWorkspace}
				active={context.active()}
				onClose={context.onLeave}
				onFocusRequest={context.onFocusRequest}
				onOpenRelease={openExternal}
				onOpenError={(error) =>
					context.showToast({
						title: "Could not open release",
						subtitle: error instanceof Error ? error.message : String(error),
						variant: "error",
					})
				}
			/>
		),
	},
	scratchpad: {
		kind: "scratchpad",
		identity: () => "scratchpad",
		label: () => "Scratchpad",
		closable: false,
		render: (_pane, context) => (
			<ScratchpadPanel
				controller={context.scratchpad}
				active={context.active()}
				onClose={context.onLeave}
				onFocusRequest={context.onFocusRequest}
			/>
		),
	},
	subagents: {
		kind: "subagents",
		identity: () => "subagents",
		label: () => "Sub-agents",
		closable: true,
		available: (context) => context.subagentsData() !== null,
		render: (_pane, context) => {
			const data = context.subagentsData();
			if (!data) return <box />;
			return (
				<SubagentsPanel
					data={context.subagentsData}
					openOverlay={context.openOverlay}
					active={context.active()}
					onClose={context.onLeave}
					onFocusRequest={context.onFocusRequest}
					onOpenAgent={context.openSubagent}
				/>
			);
		},
	},
	subagent: {
		kind: "subagent",
		identity: (pane) => `subagent:${pane.agentName}`,
		label: (pane) => pane.agentName,
		closable: true,
		available: (context) => context.subagentsData() !== null,
		render: (pane, context) => {
			const data = context.subagentsData();
			if (!data) return <box />;
			return (
				<SubagentPanel
					agentName={pane.agentName}
					data={context.subagentsData}
					openOverlay={context.openOverlay}
					active={context.active()}
					onBack={context.openSubagents}
					onDismissed={context.closePane}
					onFocusRequest={context.onFocusRequest}
				/>
			);
		},
	},
} satisfies WorkspacePaneDefinitionMap;

function definitionFor<K extends WorkspacePaneKind>(
	kind: K,
): WorkspacePaneDefinition<K> {
	return WORKSPACE_PANE_DEFINITIONS[kind] as WorkspacePaneDefinitionMap[K];
}

export function workspacePaneIdentity(pane: WorkspacePane): string {
	const definition = definitionFor(
		pane.kind,
	) as WorkspacePaneDefinition<WorkspacePaneKind>;
	return definition.identity(pane);
}

export function workspacePaneLabel(
	pane: WorkspacePane,
	tabs: readonly { pane: WorkspacePane }[],
): string {
	const definition = definitionFor(
		pane.kind,
	) as WorkspacePaneDefinition<WorkspacePaneKind>;
	return definition.label(pane, tabs);
}

export function workspacePaneClosable(pane: WorkspacePane): boolean {
	return definitionFor(pane.kind).closable;
}

export function workspacePaneAvailable(
	pane: WorkspacePane,
	context: WorkspacePaneRenderContext,
): boolean {
	return definitionFor(pane.kind).available?.(context) ?? true;
}

export function WorkspacePaneSurface(props: {
	pane: WorkspacePane;
	selected: () => boolean;
	context: WorkspacePaneRenderContext;
}): JSX.Element {
	const definition = definitionFor(
		props.pane.kind,
	) as WorkspacePaneDefinition<WorkspacePaneKind>;
	return (
		<box
			position="absolute"
			left={props.selected() ? 0 : -1000}
			top={0}
			width="100%"
			height="100%"
			onMouseDown={(event) => {
				if (event.button === 0) props.context.onFocusRequest();
			}}
		>
			{definition.render(props.pane, props.context)}
		</box>
	);
}
