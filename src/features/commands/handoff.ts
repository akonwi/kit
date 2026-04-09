import type { Command } from "./types";

export const handoffCommand: Command = {
	name: "handoff",
	argName: "message",
	description: "Fork the current session into a linked child session",
	async execute({ runtime, args }) {
		try {
			await runtime.handoffSession(args);
		} catch (error) {
			runtime.emitError("Handoff failed", [
				error instanceof Error ? error.message : String(error),
			]);
		}
	},
};
