import type { JSX } from "solid-js";
import type { OverlayComponentProps } from "../app/overlay-ui";
import type { CommandRegistry } from "../features/commands";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { LoadedSettings } from "../settings";
import type { AttachmentsController } from "../shell/attachments-controller";

export type TranscriptViewport = {
	width: number;
	height: number;
};

export type PluginUI = {
	notify: (message: string, type?: "info" | "warning" | "error") => void;
	custom: <T>(
		component: (props: OverlayComponentProps<T>) => JSX.Element,
	) => Promise<T>;
	getTranscriptViewport: () => TranscriptViewport | null;
};

export type PluginContext = {
	runtime: AgentRuntime;
	commands: CommandRegistry;
	settings: LoadedSettings;
	ui: PluginUI;
	attachments: AttachmentsController;
};
