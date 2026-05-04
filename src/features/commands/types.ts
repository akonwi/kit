import type { JSX } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { AttachmentsController } from "../../shell/attachments-controller";
import type { PaletteManager } from "../../state/palette-manager";
import type { ToastInput } from "../../state/toasts";

export type CommandContext = {
	runtime: AgentRuntime;
	palette: PaletteManager;
	args: string;
	toast: (toast: ToastInput) => void;
	attachments: AttachmentsController;
	_reload: () => Promise<void>;
	openCustomOverlay: <T>(
		component: (props: OverlayComponentProps<T>) => JSX.Element,
	) => Promise<T>;
};

export type Command = {
	name: string;
	description: string;
	argName?: string;
	execute: (ctx: CommandContext) => void | Promise<void>;
};
