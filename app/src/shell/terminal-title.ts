/**
 * Terminal title manager.
 *
 * Holds a reference to the renderer's setTerminalTitle function so it can be
 * updated from anywhere (e.g. App.tsx on session name changes) without
 * threading the renderer through the component tree.
 */

import path from "node:path";
import { HOURGLASS } from "./glyphs";

let setTitle: ((title: string) => void) | null = null;
let currentSessionName: string | undefined;
let currentCwd = "";
let turnActive = false;

export function initTerminalTitle(setter: (title: string) => void) {
	setTitle = setter;
}

export function formatTerminalTitle(
	sessionName: string | undefined,
	cwd: string,
	active = false,
): string {
	const cwdBasename = path.basename(cwd);
	const prefix = active ? `${HOURGLASS} ` : "";
	if (sessionName) {
		return `${prefix}kit - ${sessionName} - ${cwdBasename}`;
	}
	return `${prefix}kit - ${cwdBasename}`;
}

function renderTitle(): void {
	if (!setTitle || !currentCwd) return;
	setTitle(formatTerminalTitle(currentSessionName, currentCwd, turnActive));
}

export function updateTerminalTitle(
	sessionName: string | undefined,
	cwd: string,
) {
	currentSessionName = sessionName;
	currentCwd = cwd;
	renderTitle();
}

export function setTerminalTitleTurnActive(active: boolean): void {
	if (turnActive === active) return;
	turnActive = active;
	renderTitle();
}
