export { type CommandRegistry, createCommandRegistry } from "./registry";
export type { Command, CommandContext } from "./types";

import { compactCommand } from "./compact";
import { handoffCommand } from "./handoff";
import { loginCommand } from "./login";
import { logoutCommand } from "./logout";
import { modelCommand } from "./model";
import { nameCommand } from "./name";
import { newCommand } from "./new";
import { quitCommand } from "./quit";
import { reloadCommand } from "./reload";
import { codeReviewCommand } from "./review";
import { sessionCommand } from "./session";
import { sessionsManageCommand } from "./sessions-manage";
import { themeCommand } from "./theme";
import { thinkingCommand } from "./thinking";
import type { Command } from "./types";

export const BUILT_IN_COMMANDS: Command[] = [
	codeReviewCommand,
	compactCommand,
	handoffCommand,
	loginCommand,
	logoutCommand,
	modelCommand,
	nameCommand,
	newCommand,
	quitCommand,
	reloadCommand,
	sessionCommand,
	sessionsManageCommand,
	themeCommand,
	thinkingCommand,
];
