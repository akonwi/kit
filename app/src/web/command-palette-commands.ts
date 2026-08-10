import type { RemoteCommand } from "./remote-services";

export type BrowserCommandAction = "model" | "thinking";

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
