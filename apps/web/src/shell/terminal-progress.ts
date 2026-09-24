import { writeFileSync } from "node:fs";

const OSC = "\u001B]";
const ST = "\u001B\\";

export type TerminalProgressState = "indeterminate" | "remove";

export function supportsTerminalProgress(
	environment: NodeJS.ProcessEnv = process.env,
): boolean {
	// Raw OSC passthrough differs across multiplexers. Avoid emitting until Kit
	// can wrap the sequence for the active tmux/screen version.
	if (environment.TMUX || environment.STY) return false;
	return (
		environment.TERM_PROGRAM?.toLowerCase() === "ghostty" ||
		environment.TERM?.toLowerCase().includes("ghostty") === true
	);
}

export function terminalProgressSequence(state: TerminalProgressState): string {
	const value = state === "indeterminate" ? "3" : "0";
	return `${OSC}9;4;${value}${ST}`;
}

function writeTerminalSequence(sequence: string): boolean {
	try {
		writeFileSync("/dev/tty", sequence);
		return true;
	} catch {
		// Fall through to stderr when there is no controlling TTY path.
	}

	if (!process.stderr.isTTY) return false;
	try {
		process.stderr.write(sequence);
		return true;
	} catch {
		return false;
	}
}

/**
 * Update Ghostty's surface progress report. Other terminals are left alone
 * until their OSC 9;4 behavior has been verified.
 */
export function setTerminalProgress(state: TerminalProgressState): boolean {
	if (!supportsTerminalProgress()) return false;
	return writeTerminalSequence(terminalProgressSequence(state));
}
