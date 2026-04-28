import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { Turn } from "../../session/types";

export type TranscriptToast = {
	title: string;
	lines: string[];
	variant: "info" | "warning" | "error";
};

export type TranscriptPaneProps = {
	runtime: AgentRuntime;
	turns: Turn[];
	showToast: (toast: TranscriptToast) => void;
};
