import type { Command } from "./types";

type CommandRegistryListener = () => void;

export function createCommandRegistry(initial: Command[] = []) {
	let commands = [...initial];
	const listeners = new Set<CommandRegistryListener>();

	function notify(): void {
		for (const listener of listeners) listener();
	}

	function register(command: Command): () => void {
		commands = [...commands, command];
		notify();
		return () => {
			commands = commands.filter((candidate) => candidate !== command);
			notify();
		};
	}

	function getAll(): Command[] {
		return [...commands];
	}

	function subscribe(listener: CommandRegistryListener): () => void {
		listeners.add(listener);
		return () => listeners.delete(listener);
	}

	return {
		register,
		getAll,
		subscribe,
	};
}

export type CommandRegistry = ReturnType<typeof createCommandRegistry>;
