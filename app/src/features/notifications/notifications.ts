/**
 * Bell and terminal notifications on agent turn completion.
 *
 * Emits terminal BEL plus an OpenTUI notification when available, and plays
 * the macOS Funk sound for errors.
 */

import { spawn } from "node:child_process";
import { writeFileSync } from "node:fs";
import { platform } from "node:os";

const FUNK_SOUND_PATH = "/System/Library/Sounds/Funk.aiff";

type TerminalNotifier = (message: string, title?: string) => boolean;

// ── Bell ────────────────────────────────────────────────────────────

function writeBell(): void {
	// For terminal-tab bell indicators (Ghostty, iTerm, etc), write BEL directly
	// to the controlling TTY when possible.
	try {
		writeFileSync("/dev/tty", "\u0007");
		return;
	} catch {
		// fall through
	}

	// Fallback paths.
	try {
		process.stderr.write("\u0007");
		return;
	} catch {
		// fall through
	}

	try {
		process.stdout.write("\u0007");
	} catch {
		// best effort
	}
}

function playErrorSound(): void {
	if (platform() === "darwin") {
		const child = spawn("afplay", [FUNK_SOUND_PATH], { stdio: "ignore" });
		child.unref();
	}
}

function triggerTerminalNotification(
	notify: TerminalNotifier | undefined,
	message: string,
	title: string,
): boolean {
	try {
		return notify?.(message, title) === true;
	} catch {
		return false;
	}
}

/**
 * Emit BEL for terminal bell/tab indicators and also request a terminal-mediated
 * notification when available.
 */
export function ringBell(
	isError: boolean,
	options?: {
		notify?: TerminalNotifier;
		message?: string;
		title?: string;
	},
): void {
	// OpenTUI notifications are terminal/OS dependent and may be quiet or hidden
	// while focused, so keep BEL as the reliable bell/tab indicator.
	writeBell();
	triggerTerminalNotification(
		options?.notify,
		options?.message ?? (isError ? "Turn failed" : "Turn complete"),
		options?.title ?? "Kit",
	);

	if (isError) {
		playErrorSound();
	}
}
