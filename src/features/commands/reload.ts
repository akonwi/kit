import type { Command } from "./types";

export const reloadCommand: Command = {
	name: "reload",
	description: "Reload the current session and refresh plugin state",
	async execute({ _reload }) {
		await _reload();
	},
};
