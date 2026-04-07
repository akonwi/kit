export type { Command, CommandContext } from "./types";

import type { Command } from "./types";
import { quitCommand } from "./quit";

// TODO: re-add commands as features are rebuilt
// - bells/speech: needs notification config
// - compact, session, model, thinking, name: need runtime updates
// - switch, sessions-manage, handoff: need new session layer
// - pager, steer, followUp: need pager/steering features

export const COMMANDS: Command[] = [
  quitCommand,
];
