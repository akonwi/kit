import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { TranscriptItem } from "./turns";

export type TranscriptToast = {
	title: string;
	lines: string[];
	variant: "info" | "warning" | "error";
};

export type TranscriptProps = {
	runtime: AgentRuntime;
	showToast: (toast: TranscriptToast) => void;
};

export type TranscriptPaneProps = TranscriptProps & {
	items: TranscriptItem[];
};
