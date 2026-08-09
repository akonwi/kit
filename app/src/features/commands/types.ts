import type { JSX } from "solid-js";
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { AttachmentsController } from "../../shell/attachments-controller";
import type { PickerManager } from "../../state/picker-manager";
import type { ToastInput } from "../../state/toasts";
import type { ReviewDraftController } from "../review/draft-controller";
import type { ReviewWorkspaceController } from "../review/workspace-controller";

export type CommandContext = {
	runtime: AgentRuntime;
	picker: PickerManager;
	args: string;
	toast: (toast: ToastInput) => void;
	attachments: AttachmentsController;
	reviewDrafts: ReviewDraftController;
	reviewWorkspace: ReviewWorkspaceController;
	_reload: () => Promise<void>;
	openCustomOverlay: <T>(
		component: (props: OverlayComponentProps<T>) => JSX.Element,
	) => Promise<T>;
};

export type TransportNeutralCommandContext = {
	runtime: AgentRuntime;
	args: string;
	persistSessions: boolean;
	schedulePrompt(message: string): void;
	signal?: AbortSignal;
};

export type Command = {
	/** Stable canonical id used for ownership, keybindings, and execution. */
	name: string;
	/** User-facing slash command name. Defaults to `name`. */
	displayName?: string;
	description: string;
	argName?: string;
	category?: string;
	/** Execute with renderer-owned context and presentation semantics. */
	execute: (ctx: CommandContext) => void | Promise<void>;
	/** Execute without renderer-owned context when exposed through a remote host. */
	executeTransportNeutral?: (
		ctx: TransportNeutralCommandContext,
	) => void | Promise<void>;
};
