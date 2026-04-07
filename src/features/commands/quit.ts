import type { Command } from "./types";

export const quitCommand: Command = {
	name: "quit",
	description: "Exit kit",
	execute({ runtime }) {
		runtime.quit();
	},
};
