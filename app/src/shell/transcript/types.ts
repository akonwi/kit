import type { JSX } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
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
 * Opens the rich turn activity view for the given source. The AppShell
 * decides at call time whether to mount it as an inline right-side
 * sidebar (when the terminal is wide enough) or as a modal dialog.
 */
export type OpenActivity = (source: ActivitySource) => void;

export type TranscriptProps = {
	runtime: AgentRuntime;
	showToast: (toast: TranscriptToast) => void;
	openOverlay: OpenOverlay;
	openActivity: OpenActivity;
};

export type TranscriptPaneProps = TranscriptProps & {
	items: TranscriptItem[];
};
