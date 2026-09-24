import type { JSX } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { ImagePreviewSource } from "../../features/images/types";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { ActivitySource } from "./turn-activity-view";
import type { TranscriptItem } from "./turns";

export type TranscriptToast = {
	title: string;
	subtitle?: string;
	variant: "info" | "warning" | "error";
};

export type OpenOverlay = <T>(
	component: (props: OverlayComponentProps<T>) => JSX.Element,
) => Promise<T>;

/**
 * Opens the rich turn activity workspace panel for the given source.
 */
export type OpenActivity = (source: ActivitySource) => void;

export type OpenSubagent = (agentName: string) => boolean;

export type OpenImage = (request: {
	id: string;
	image: ImagePreviewSource;
}) => void;

export type OpenMessageContextMenu = (request: {
	x: number;
	y: number;
	markdown: string;
}) => void;

export type TranscriptProps = {
	runtime: AgentRuntime;
	showToast: (toast: TranscriptToast) => void;
	openOverlay: OpenOverlay;
	openActivity: OpenActivity;
	openSubagent: OpenSubagent;
	openImage: OpenImage;
	openMessageContextMenu: OpenMessageContextMenu;
};

export type TranscriptPaneProps = TranscriptProps & {
	items: TranscriptItem[];
	expandedImageToolCallIds?: ReadonlySet<string>;
	setImageToolCallExpanded?: (toolCallId: string, expanded: boolean) => void;
};
