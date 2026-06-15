export { type CommandRegistry, createCommandRegistry } from "./registry";
export type { Command, CommandContext } from "./types";

import { compactCommand } from "./compact";
import { handoffCommand } from "./handoff";
import { loginCommand } from "./login";
import { modelCommand } from "./model";
import { nameCommand } from "./name";
import { newCommand } from "./new";
import { queueEditorCommand } from "./queue-editor";
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
	modelCommand,
	nameCommand,
	newCommand,
	queueEditorCommand,
	quitCommand,
	reloadCommand,
	sessionCommand,
	sessionsManageCommand,
	themeCommand,
	thinkingCommand,
];
