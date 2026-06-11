import { createComponent } from "solid-js";
import { QueueEditorDialog } from "../../shell/QueueEditorDialog";
import type { Command } from "./types";

export const queueEditorCommand: Command = {
	name: "edit-queue",
	description: "Edit queued messages",
	execute({ runtime, toast, openCustomOverlay }) {
		if (runtime.getPendingMessageCount() === 0) {
			toast({
				title: "No queued messages",
				subtitle:
					"Queue a follow-up while the agent is working to edit it here.",
				variant: "info",
			});
			return;
		}

		void openCustomOverlay<void>((props) =>
			createComponent(QueueEditorDialog, {
				runtime,
				done: props.done,
				surfaceProps: props.surfaceProps,
				active: props.active,
			}),
		);
	},
};
