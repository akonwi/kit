import type { Command } from "./types";

export const handoffCommand: Command = {
	name: "handoff",
	description: "Fork the current session into a linked child session",
	async execute({ runtime }) {
		try {
			await runtime.handoffSession();
		} catch (error) {
			runtime.emitError("Handoff failed", [
				error instanceof Error ? error.message : String(error),
			]);
		}
	},
};
