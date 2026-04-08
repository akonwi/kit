export type { Command, CommandContext } from "./types";

import { loginCommand } from "./login";
import { modelCommand } from "./model";
import { nameCommand } from "./name";
import { newCommand } from "./new";
import { quitCommand } from "./quit";
import { sessionCommand } from "./session";
import { switchCommand } from "./switch";
import { thinkingCommand } from "./thinking";
import type { Command } from "./types";

export const COMMANDS: Command[] = [
	loginCommand,
	modelCommand,
	nameCommand,
	newCommand,
	sessionCommand,
	switchCommand,
	thinkingCommand,
	quitCommand,
];
