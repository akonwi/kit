import type { Command } from "./types";

export const bellsCommand: Command = {
	name: "bells",
	description: "Toggle audible notification sounds on/off",
	async execute({ runtime }) {
		await runtime.toggleBells();
	},
};

export const speechCommand: Command = {
	name: "speech",
	description: "Toggle the agent's speech notifications",
	async execute({ runtime }) {
		await runtime.toggleSpeech();
	},
};
