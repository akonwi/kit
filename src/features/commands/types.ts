import type { JSX } from "solid-js";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { PaletteManager } from "../../state/palette-manager";

export type CommandContext = {
	runtime: AgentRuntime;
	palette: PaletteManager;
	args: string;
	openCustomOverlay: <T>(
		component: (props: { done: (result: T) => void }) => JSX.Element,
	) => Promise<T>;
};

export type Command = {
	name: string;
	description: string;
	argName?: string;
	execute: (ctx: CommandContext) => void | Promise<void>;
};
