import { spawn } from "child_process";
import { platform } from "os";

/**
 * Write text to clipboard via OSC 52 escape sequence.
 * This lets clipboard operations work over SSH — the terminal
 * emulator handles the clipboard locally.
 */
function writeOsc52(text: string): void {
	if (!process.stdout.isTTY) return;
	const base64 = Buffer.from(text).toString("base64");
	const osc52 = `\x1b]52;c;${base64}\x07`;
	const passthrough = process.env["TMUX"] || process.env["STY"];
	const sequence = passthrough ? `\x1bPtmux;\x1b${osc52}\x1b\\` : osc52;
	process.stdout.write(sequence);
}

async function nativeCopy(text: string): Promise<void> {
	const os = platform();

	if (os === "darwin") {
		return new Promise((resolve, reject) => {
			const proc = spawn("pbcopy", { stdio: ["pipe", "ignore", "ignore"] });
			proc.on("error", reject);
			proc.on("close", (code) =>
				code === 0 ? resolve() : reject(new Error(`pbcopy exited ${code}`)),
			);
			proc.stdin.end(text);
		});
	}

	if (os === "linux") {
		// Try wayland first, then X11
		const cmd = process.env["WAYLAND_DISPLAY"]
			? ["wl-copy"]
			: ["xclip", "-selection", "clipboard"];
		return new Promise((resolve, reject) => {
			const proc = spawn(cmd[0], cmd.slice(1), {
				stdio: ["pipe", "ignore", "ignore"],
			});
			proc.on("error", reject);
			proc.on("close", (code) =>
				code === 0 ? resolve() : reject(new Error(`${cmd[0]} exited ${code}`)),
			);
			proc.stdin.end(text);
		});
	}

	// Fallback: OSC 52 already written above
}

export async function copyToClipboard(text: string): Promise<void> {
	writeOsc52(text);
	await nativeCopy(text);
}
