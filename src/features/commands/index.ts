export type { Command, CommandContext } from "./types";

import { loginCommand } from "./login";
import { modelCommand } from "./model";
import { quitCommand } from "./quit";
import { toastTestCommand } from "./toast-test";
import type { Command } from "./types";

export const COMMANDS: Command[] = [loginCommand, modelCommand, toastTestCommand, quitCommand];
