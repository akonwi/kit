import type { Command } from "./types";

export const quitCommand: Command = {
	name: "/quit",
	description: "Exit pi-kit",
	execute({ runtime }) {
		runtime.quit();
	},
};
