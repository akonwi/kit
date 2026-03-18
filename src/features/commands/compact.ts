import type { Command } from "./types";

export const compactCommand: Command = {
	name: "compact",
	description: "Compact session context",
	async execute({ runtime }) {
		const agentSession = runtime.getAgentSession();
		runtime.showPanel("Compacting...");
		try {
			await agentSession.compact();
		} catch (err) {
			runtime.emitError("compact", [
				err instanceof Error ? err.message : String(err),
			]);
		} finally {
			runtime.hidePanel();
			runtime.refreshStatus();
		}
	},
};
