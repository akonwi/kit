import type { JSX } from "solid-js";
import type { OverlayComponentProps } from "../app/overlay-ui";
import type { CommandRegistry } from "../features/commands";
import type { AgentRuntime } from "../runtime/agent-runtime";
import type { LoadedSettings } from "../settings";
import type { AttachmentsController } from "../shell/attachments-controller";
import type { ToastInput } from "../state/toasts";

export type TranscriptViewport = {
	width: number;
	height: number;
};

export type PluginUI = {
	toast: (toast: ToastInput) => void;
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
