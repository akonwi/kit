import type { Command } from "./types";

export const compactCommand: Command = {
	name: "compact",
	description: "Compact session context to reduce token usage",
	async execute({ runtime }) {
		await runtime.compact();
	},
};
