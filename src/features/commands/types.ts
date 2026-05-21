import type { JSX } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { AttachmentsController } from "../../shell/attachments-controller";
import type { PickerManager } from "../../state/picker-manager";
import type { ToastInput } from "../../state/toasts";

export type CommandContext = {
	runtime: AgentRuntime;
	picker: PickerManager;
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
	category?: string;
	execute: (ctx: CommandContext) => void | Promise<void>;
};
