export type CommandDef = {
  name: string;
  description: string;
  /** If true, the command takes arguments after the name */
  takesArgs?: boolean;
};

export const COMMANDS: CommandDef[] = [
  { name: "/new", description: "Start a new session" },
  { name: "/model", description: "Select a model" },
  { name: "/thinking", description: "Cycle or set thinking level", takesArgs: true },
  { name: "/name", description: "Set session display name", takesArgs: true },
  { name: "/session", description: "Show session stats" },
  { name: "/quit", description: "Exit pi-kit" },
];

export function matchCommands(query: string): CommandDef[] {
  const q = query.toLowerCase();
  if (!q || q === "/") return COMMANDS;
  return COMMANDS.filter((c) => c.name.toLowerCase().includes(q));
}
