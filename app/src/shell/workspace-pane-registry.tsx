import type { JSX } from "solid-js";
import {
	MERMAID_PREVIEW_MIN_COLS,
	MermaidPreviewPanel,
} from "../features/mermaid-preview/MermaidPreviewPanel";
import {
	RELEASE_NOTES_MIN_COLS,
	ReleaseNotesPanel,
	type ReleasesWorkspaceController,
} from "../features/releases";
import type { ReviewDraftController } from "../features/review/draft-controller";
import { ReviewContent } from "../features/review/ReviewContent";
import type { ScratchpadController } from "../features/scratchpad/controller";
import {
	SCRATCHPAD_MIN_COLS,
	ScratchpadPanel,
} from "../features/scratchpad/ScratchpadPanel";
import type { SubagentsPanelData } from "../features/subagents";
import { SubagentPanel } from "../features/subagents/SubagentPanel";
import {
	SUBAGENTS_MIN_COLS,
	SubagentsPanel,
} from "../features/subagents/SubagentsPanel";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { ReviewDiffView } from "../settings";
import type { ToastInput } from "../state/toasts";
import type { AttachmentsController } from "./attachments-controller";
import { openExternal } from "./open-external";
import { TurnActivityPanel } from "./transcript/TurnActivityPanel";
import type { ActivitySource } from "./transcript/turn-activity-view";
import type { OpenOverlay } from "./transcript/types";

const ACTIVITY_MIN_COLS = 40;
const REVIEW_MIN_COLS = 60;

export type WorkspacePane =
	| { kind: "activity"; source: ActivitySource }
	| { kind: "mermaid"; source: string }
	| { kind: "review" }
	| { kind: "releases" }
	| { kind: "scratchpad" }
	| { kind: "subagents" }
	| { kind: "subagent"; agentName: string };

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
	attachments: AttachmentsController;
	reviewDrafts: ReviewDraftController;
	defaultReviewDiffView: () => ReviewDiffView;
	onReviewDiffViewChanged: (view: ReviewDiffView) => void;
	onSubmitReviewMessage: () => void | Promise<void>;
	releasesWorkspace: ReleasesWorkspaceController;
	scratchpad: ScratchpadController;
	subagentsData: () => SubagentsPanelData | null;
	openSubagents: () => void;
	openSubagent: (agentName: string) => void;
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
	minColumns: number;
	available?: (context: WorkspacePaneRenderContext) => boolean;
	render: (
		pane: PaneOfKind<K>,
		context: WorkspacePaneRenderContext,
	) => JSX.Element;
};

type WorkspacePaneDefinitionMap = {
	[K in WorkspacePaneKind]: WorkspacePaneDefinition<K>;
};

export const WORKSPACE_PANE_DEFINITIONS = {
	activity: {
		kind: "activity",
		identity: () => "activity",
		label: () => "Activity",
		closable: true,
		minColumns: ACTIVITY_MIN_COLS,
		render: (pane, context) => (
			<TurnActivityPanel
				runtime={context.runtime}
				source={pane.source}
				active={context.active()}
				onClose={context.onLeave}
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
		minColumns: MERMAID_PREVIEW_MIN_COLS,
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
	review: {
		kind: "review",
		identity: () => "review",
		label: () => "Code review",
		closable: false,
		minColumns: REVIEW_MIN_COLS,
		render: (_pane, context) => (
			<ReviewContent
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
			/>
		),
	},
	releases: {
		kind: "releases",
		identity: () => "releases",
		label: () => "Release notes",
		closable: true,
		minColumns: RELEASE_NOTES_MIN_COLS,
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
		minColumns: SCRATCHPAD_MIN_COLS,
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
		minColumns: SUBAGENTS_MIN_COLS,
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
		minColumns: SUBAGENTS_MIN_COLS,
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

export function workspacePaneMinColumns(pane: WorkspacePane): number {
	return definitionFor(pane.kind).minColumns;
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
