import type { Command } from "./types";

export const reloadCommand: Command = {
	name: "reload",
	description: "Reload the current session and refresh discovered context",
	async execute({ runtime }) {
		await runtime.reloadSession();
	},
};
