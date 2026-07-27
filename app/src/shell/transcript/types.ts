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
 * Opens the rich turn activity workspace panel for the given source.
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
