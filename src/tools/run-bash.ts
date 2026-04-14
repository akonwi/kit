/**
 * Low-level bash execution. Used by both the agent bash tool and
 * the user `!command` / `!!command` feature.
 */

import { spawn } from "node:child_process";

const DEFAULT_TIMEOUT_MS = 120_000;

function killTree(pid: number): void {
	try {
		process.kill(-pid, "SIGTERM");
		setTimeout(() => {
			try {
				process.kill(-pid, "SIGKILL");
			} catch {
				/* already dead */
			}
		}, 5_000).unref();
	} catch {
		// Process already gone
	}
}

export interface BashExecResult {
	output: string;
	exitCode: number | undefined;
}

export function runBash(
	command: string,
	cwd: string,
	timeoutMs = DEFAULT_TIMEOUT_MS,
	signal?: AbortSignal,
): Promise<BashExecResult> {
	return new Promise((resolve) => {
		const shell = process.env.SHELL || "bash";
		const proc = spawn(shell, ["-c", command], {
			cwd,
			detached: true,
			stdio: ["ignore", "pipe", "pipe"],
		});

		let output = "";
		proc.stdout.on("data", (d: Buffer) => {
			output += d.toString();
		});
		proc.stderr.on("data", (d: Buffer) => {
			output += d.toString();
		});

		const kill = () => {
			if (proc.pid != null) killTree(proc.pid);
		};

		const timer = setTimeout(() => {
			output += "\n[timed out]";
			kill();
		}, timeoutMs);

		signal?.addEventListener("abort", kill, { once: true });

		proc.on("close", (code) => {
			clearTimeout(timer);
			signal?.removeEventListener("abort", kill);
			resolve({ output: output.trimEnd(), exitCode: code ?? undefined });
		});

		proc.on("error", (err) => {
			clearTimeout(timer);
			signal?.removeEventListener("abort", kill);
			resolve({ output: err.message, exitCode: undefined });
		});
	});
}