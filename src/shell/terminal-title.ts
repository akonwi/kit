/**
 * Terminal title manager.
 *
 * Holds a reference to the renderer's setTerminalTitle function so it can be
 * updated from anywhere (e.g. App.tsx on session name changes) without
 * threading the renderer through the component tree.
 */

import path from "node:path";

let setTitle: ((title: string) => void) | null = null;

export function initTerminalTitle(setter: (title: string) => void) {
	setTitle = setter;
}

function formatTitle(sessionName: string | undefined, cwd: string): string {
	const cwdBasename = path.basename(cwd);
	if (sessionName) {
		return `pi-kit - ${sessionName} - ${cwdBasename}`;
	}
	return `pi-kit - ${cwdBasename}`;
}

export function updateTerminalTitle(sessionName: string | undefined, cwd: string) {
	if (!setTitle) return;
	setTitle(formatTitle(sessionName, cwd));
}
