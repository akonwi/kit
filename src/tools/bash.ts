import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { AgentTool } from "@mariozechner/pi-agent-core";
import { Type } from "@mariozechner/pi-ai";

const MAX_OUTPUT_CHARS = 30_000;
const TIMEOUT_MS = 120_000;

export interface BashResult {
	output: string;
	exitCode: number | undefined;
	truncated: boolean;
}

export function createBashTool(cwd: string): AgentTool<any> {
	return {
		name: "bash",
		label: "Bash",
		description:
			"Run a shell command in the project directory. Use for file operations, building, testing, git, etc. Avoid interactive commands.",
		parameters: Type.Object({
			command: Type.String({ description: "The shell command to run" }),
			timeout: Type.Optional(
				Type.Number({
					description: "Timeout in milliseconds (default 120000)",
				}),
			),
		}),
		async execute(_id, params, signal) {
			const timeoutMs = params.timeout ?? TIMEOUT_MS;
			let tmpDir: string | null = null;
			let fullOutputPath: string | undefined;

			try {
				const result = await runBash(params.command, cwd, timeoutMs, signal);

				let output = result.output;
				let truncated = false;

				if (output.length > MAX_OUTPUT_CHARS) {
					tmpDir = await mkdtemp(join(tmpdir(), "kit-bash-"));
					fullOutputPath = join(tmpDir, "output.txt");
					await writeFile(fullOutputPath, output, "utf8");
					output = output.slice(0, MAX_OUTPUT_CHARS);
					truncated = true;
				}

				const exitLine =
					result.exitCode !== 0 && result.exitCode != null
						? `\n[exit code: ${result.exitCode}]`
						: "";
				const truncLine = truncated
					? `\n[output truncated — full output at ${fullOutputPath}]`
					: "";

				return {
					content: [{ type: "text", text: `${output}${exitLine}${truncLine}` }],
					details: { exitCode: result.exitCode, truncated },
				};
			} catch (err) {
				const msg = err instanceof Error ? err.message : String(err);
				return {
					content: [{ type: "text", text: `Error: ${msg}` }],
					details: { exitCode: undefined, truncated: false },
					isError: true,
				} as any;
			}
		},
	};
}

function killTree(pid: number): void {
	try {
		// Negative PID sends signal to the entire process group
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

function runBash(
	command: string,
	cwd: string,
	timeoutMs: number,
	signal?: AbortSignal,
): Promise<{ output: string; exitCode: number | undefined }> {
	return new Promise((resolve) => {
		const shell = process.env.SHELL || "bash";
		const proc = spawn(shell, ["-c", command], {
			cwd,
			// detached: true creates a new process group so we can kill the whole tree
			detached: true,
			stdio: ["ignore", "pipe", "pipe"],
		});

		let output = "";
		proc.stdout.on("data", (d: Buffer) => (output += d.toString()));
		proc.stderr.on("data", (d: Buffer) => (output += d.toString()));

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
