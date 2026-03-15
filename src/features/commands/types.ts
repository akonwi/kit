import type { AgentRuntime } from "../../backend";
import type { PaletteManager } from "../../state/palette-manager";

export type CommandContext = {
	runtime: AgentRuntime;
	palette: PaletteManager;
};

export type Command = {
	name: string;
	description: string;
	execute: (ctx: CommandContext) => void | Promise<void>;
};
