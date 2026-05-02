import { createComponent } from "solid-js";
import { SessionExplorerModal } from "../sessions/SessionExplorerModal";
import type { Command } from "./types";

export const treeCommand: Command = {
	name: "tree",
	description: "Browse the current session tree in a modal explorer",
	async execute({ runtime, toast, openCustomOverlay }) {
		const selectedId = await openCustomOverlay<string | null>((props) =>
			createComponent(SessionExplorerModal, {
				runtime,
				toast,
				onClose: () => props.done(null),
				onSelect: (sessionId) => props.done(sessionId),
			}),
		);
		if (!selectedId || selectedId === runtime.getSession().id) return;
		try {
			await runtime.switchSession(selectedId);
		} catch (error) {
			toast({
				title: "Session switch failed",
				lines: [String(error)],
				variant: "error",
			});
		}
	},
};
