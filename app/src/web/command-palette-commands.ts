import type { RemoteCommand } from "./remote-services";

export type BrowserCommandAction =
	| "model"
	| "theme"
	| "thinking"
	| "toggle-scratchpad"
	| "open-code-review";

export type PaletteCommand = RemoteCommand & {
	browserAction?: BrowserCommandAction;
};

export const BROWSER_COMMANDS: readonly PaletteCommand[] = [
	{
		id: "model",
		name: "model",
		description: "Switch the active model",
		browserAction: "model",
	},
	{
		id: "thinking",
		name: "thinking",
		description: "Set thinking level",
		browserAction: "thinking",
	},
	{
		id: "theme",
		name: "theme",
		description: "Switch the browser color theme",
		browserAction: "theme",
	},
	{
		id: "toggle-scratchpad",
		name: "toggle-scratchpad",
		description: "Toggle scratchpad",
		browserAction: "toggle-scratchpad",
	},
	{
		id: "open-code-review",
		name: "code-review",
		description: "Review the current changes",
		browserAction: "open-code-review",
	},
];

export function mergePaletteCommands(
	remoteCommands: readonly RemoteCommand[],
): PaletteCommand[] {
	const browserCommandIds = new Set(
		BROWSER_COMMANDS.map((command) => command.id),
	);
	return [
		...BROWSER_COMMANDS,
		...remoteCommands.filter((command) => !browserCommandIds.has(command.id)),
	];
}
