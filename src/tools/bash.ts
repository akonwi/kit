import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { AgentTool } from "@earendil-works/pi-agent-core";
import { Type } from "@earendil-works/pi-ai";
import { runBash } from "./run-bash";

const MAX_OUTPUT_CHARS = 30_000;
const TIMEOUT_MS = 120_000;

// Extracted so the return type can reference `typeof parameters` instead of `AgentTool<any>`.
const parameters = Type.Object({
	command: Type.String({ description: "The shell command to run" }),
	timeout: Type.Optional(
		Type.Number({ description: "Timeout in milliseconds (default 120000)" }),
	),
});

export function createBashTool(cwd: string): AgentTool<typeof parameters> {
	return {
		name: "bash",
		label: "Bash",
		description:
			"Run a shell command in the project directory. Use for file operations, building, testing, git, etc. Avoid interactive commands.",
		parameters,
		async execute(_id, params, signal) {
			const timeoutMs = params.timeout ?? TIMEOUT_MS;
			let fullOutputPath: string | undefined;

			try {
				const result = await runBash(params.command, cwd, timeoutMs, signal);

				let output = result.output;
				let truncated = false;

				if (output.length > MAX_OUTPUT_CHARS) {
					const tmpDir = await mkdtemp(join(tmpdir(), "kit-bash-"));
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
				};
			}
		},
	};
}
