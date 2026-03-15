import type { Command } from "./types";

export const newCommand: Command = {
	name: "/new",
	description: "Start a new session",
	async execute({ runtime }) {
		await runtime.newSession();
	},
};
