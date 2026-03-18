export type { Command, CommandContext } from "./types";

import type { Command } from "./types";
import { bellsCommand, speechCommand } from "./bells-speech";
import { compactCommand } from "./compact";
import { newCommand } from "./new";
import { modelCommand } from "./model";
import { thinkingCommand } from "./thinking";
import { nameCommand } from "./name";
import { switchCommand } from "./switch";
import { sessionsManageCommand } from "./sessions-manage";
import { handoffCommand } from "./handoff";
import { pagerCommand } from "./pager";
import { quitCommand } from "./quit";

export const COMMANDS: Command[] = [
	bellsCommand,
	compactCommand,
	speechCommand,
	newCommand,
	modelCommand,
	thinkingCommand,
	nameCommand,
	switchCommand,
	sessionsManageCommand,
	handoffCommand,
	pagerCommand,
	quitCommand,
] as const;
