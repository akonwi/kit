import { createComponent } from "solid-js";
import { SessionExplorerModal } from "../sessions/SessionExplorerModal";
import type { Command } from "./types";

export const sessionsManageCommand: Command = {
	name: "sessions",
	description: "Browse, switch, rename, delete, or squash sessions",
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
