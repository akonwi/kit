import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { PaletteManager } from "../../state/palette-manager";
import type { GuidedQuestionsController } from "../guided-questions";

export type CommandContext = {
	runtime: AgentRuntime;
	palette: PaletteManager;
	guidedQuestions: GuidedQuestionsController;
	args: string;
};

export type Command = {
	name: string;
	description: string;
	argName?: string;
	execute: (ctx: CommandContext) => void | Promise<void>;
};
