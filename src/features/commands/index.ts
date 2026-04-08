export type { Command, CommandContext } from "./types";

import { bellsCommand } from "./bells-speech";
import { loginCommand } from "./login";
import { modelCommand } from "./model";
import { nameCommand } from "./name";
import { newCommand } from "./new";
import { quitCommand } from "./quit";
import { sessionCommand } from "./session";
import { sessionsManageCommand } from "./sessions-manage";
import { thinkingCommand } from "./thinking";
import type { Command } from "./types";

export const COMMANDS: Command[] = [
	bellsCommand,
	loginCommand,
	modelCommand,
	nameCommand,
	newCommand,
	sessionCommand,
	sessionsManageCommand,
	thinkingCommand,
	quitCommand,
];
