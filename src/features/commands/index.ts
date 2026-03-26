export type { Command, CommandContext } from "./types";

import type { Command } from "./types";
import { bellsCommand, speechCommand } from "./bells-speech";
import { compactCommand } from "./compact";
import { newCommand } from "./new";
import { sessionCommand } from "./session";
import { modelCommand } from "./model";
import { thinkingCommand } from "./thinking";
import { nameCommand } from "./name";
import { switchCommand } from "./switch";
import { sessionsManageCommand } from "./sessions-manage";
import { handoffCommand } from "./handoff";
import { pagerCommand } from "./pager";
import { quitCommand } from "./quit";
import { steerCommand, followUpCommand } from "./steering";

export const COMMANDS: Command[] = [
	bellsCommand,
	compactCommand,
	speechCommand,
	newCommand,
	modelCommand,
	thinkingCommand,
	nameCommand,
	switchCommand,
	sessionCommand,
	sessionsManageCommand,
	handoffCommand,
	pagerCommand,
	quitCommand,
	steerCommand,
	followUpCommand,
] as const;
