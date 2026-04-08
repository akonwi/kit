export type { Command, CommandContext } from "./types";

import { loginCommand } from "./login";
import { modelCommand } from "./model";
import { newCommand } from "./new";
import { quitCommand } from "./quit";
import { thinkingCommand } from "./thinking";
import type { Command } from "./types";

export const COMMANDS: Command[] = [
	loginCommand,
	modelCommand,
	newCommand,
	thinkingCommand,
	quitCommand,
];
