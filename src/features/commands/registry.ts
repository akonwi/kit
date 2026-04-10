import type { Command } from "./types";

export function createCommandRegistry(initial: Command[] = []) {
	let commands = [...initial];

	function register(command: Command): () => void {
		commands = [...commands, command];
		return () => {
			commands = commands.filter((candidate) => candidate !== command);
		};
	}

	function getAll(): Command[] {
		return [...commands];
	}

	return {
		register,
		getAll,
	};
}

export type CommandRegistry = ReturnType<typeof createCommandRegistry>;
