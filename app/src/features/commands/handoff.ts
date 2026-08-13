import type { Command } from "./types";

export const handoffCommand: Command = {
	name: "handoff",
	argName: "message",
	description: "Fork the current session into a linked child session",
	async execute({ runtime, args, toast, persistSessions }) {
		try {
			await runtime.handoffSession(args, { persist: persistSessions });
		} catch (error) {
			toast({
				title: "Handoff failed",
				subtitle: error instanceof Error ? error.message : String(error),
				variant: "error",
			});
		}
	},
	async executeTransportNeutral({
		runtime,
		args,
		persistSessions,
		schedulePrompt,
		signal,
	}) {
		await runtime.handoffSession(undefined, {
			persist: persistSessions,
			signal,
		});
		const prompt = args.trim();
		if (prompt) schedulePrompt(prompt);
	},
};
