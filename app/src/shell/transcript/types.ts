import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { TranscriptItem } from "./turns";

export type TranscriptToast = {
	title: string;
	subtitle?: string;
	variant: "info" | "warning" | "error";
};

export type TranscriptProps = {
	runtime: AgentRuntime;
	showToast: (toast: TranscriptToast) => void;
	zenMode?: boolean;
};

export type TranscriptPaneProps = TranscriptProps & {
	items: TranscriptItem[];
};
