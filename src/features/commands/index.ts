export { type CommandRegistry, createCommandRegistry } from "./registry";
export type { Command, CommandContext } from "./types";

import { handoffCommand } from "./handoff";
import { loginCommand } from "./login";
import { modelCommand } from "./model";
import { nameCommand } from "./name";
import { newCommand } from "./new";
import { quitCommand } from "./quit";
import { reloadCommand } from "./reload";
import { codeReviewCommand } from "./review";
import { sessionCommand } from "./session";
import { sessionsManageCommand } from "./sessions-manage";
import { thinkingCommand } from "./thinking";
import { treeCommand } from "./tree";
import type { Command } from "./types";

export const BUILT_IN_COMMANDS: Command[] = [
	handoffCommand,
	loginCommand,
	modelCommand,
	nameCommand,
	newCommand,
	reloadCommand,
	codeReviewCommand,
	sessionCommand,
	sessionsManageCommand,
	treeCommand,
	thinkingCommand,
	quitCommand,
];
