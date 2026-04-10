export type { Command, CommandContext } from "./types";

import { bellsCommand, speechCommand } from "./bells-speech";
import { handoffCommand } from "./handoff";
import { loginCommand } from "./login";
import { modelCommand } from "./model";
import { nameCommand } from "./name";
import { newCommand } from "./new";
import { pagerCommand } from "./pager";
import { quitCommand } from "./quit";
import { sessionCommand } from "./session";
import { sessionsManageCommand } from "./sessions-manage";
import { thinkingCommand } from "./thinking";
import type { Command } from "./types";

export const COMMANDS: Command[] = [
	bellsCommand,
	speechCommand,
	handoffCommand,
	loginCommand,
	modelCommand,
	nameCommand,
	newCommand,
	sessionCommand,
	sessionsManageCommand,
	thinkingCommand,
	pagerCommand,
	quitCommand,
];
