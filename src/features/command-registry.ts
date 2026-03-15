export type CommandDef = {
	name: string;
	description: string;
};

export const COMMANDS: CommandDef[] = [
	{ name: "/new", description: "Start a new session" },
	{ name: "/model", description: "Select a model" },
	{ name: "/thinking", description: "Cycle or set thinking level" },
	{ name: "/name", description: "Set session display name" },
	{ name: "/switch", description: "Switch to another session" },
	{ name: "/sessions:manage", description: "Rename or delete sessions" },
	{ name: "/quit", description: "Exit pi-kit" },
];
