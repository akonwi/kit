import type { JSX } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { TranscriptItem } from "./turns";

export type TranscriptToast = {
	title: string;
	subtitle?: string;
	variant: "info" | "warning" | "error";
};

export type OpenOverlay = <T>(
	component: (props: OverlayComponentProps<T>) => JSX.Element,
) => Promise<T>;

export type TranscriptProps = {
	runtime: AgentRuntime;
	showToast: (toast: TranscriptToast) => void;
	openOverlay: OpenOverlay;
};

export type TranscriptPaneProps = TranscriptProps & {
	items: TranscriptItem[];
};
