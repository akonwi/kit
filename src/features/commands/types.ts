import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { PaletteManager } from "../../state/palette-manager";

export type CommandContext = {
	runtime: AgentRuntime;
	palette: PaletteManager;
	args: string;
};

export type Command = {
	name: string;
	description: string;
	argName?: string;
	execute: (ctx: CommandContext) => void | Promise<void>;
};
