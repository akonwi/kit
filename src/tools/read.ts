import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import type { AgentTool } from "@mariozechner/pi-agent-core";
import { Type } from "@mariozechner/pi-ai";

const MAX_CHARS = 50_000;

// Extracted so the return type can reference `typeof parameters` instead of `AgentTool<any>`.
const parameters = Type.Object({
	path: Type.String({
		description: "Path to the file (relative to cwd or absolute)",
	}),
	offset: Type.Optional(
		Type.Number({
			description: "Line number to start reading from (1-indexed)",
		}),
	),
	limit: Type.Optional(
		Type.Number({ description: "Maximum number of lines to read" }),
	),
});

export function createReadTool(cwd: string): AgentTool<typeof parameters> {
	return {
		name: "read",
		label: "Read",
		description:
			"Read the contents of a file. Supports an optional line offset and limit.",
		parameters,
		async execute(_id, params, _signal) {
			try {
				const abs = resolve(cwd, params.path);
				const raw = await readFile(abs, "utf8");
				const lines = raw.split("\n");

				const start =
					params.offset != null ? Math.max(0, params.offset - 1) : 0;
				const end = params.limit != null ? start + params.limit : lines.length;
				const selected = lines.slice(start, end);

				let text = selected.join("\n");
				let truncated = false;

				if (text.length > MAX_CHARS) {
					text = `${text.slice(0, MAX_CHARS)}\n[truncated]`;
					truncated = true;
				}

				return {
					content: [{ type: "text", text }],
					details: { path: abs, lines: selected.length, truncated },
				};
			} catch (err) {
				const msg = err instanceof Error ? err.message : String(err);
				return {
					content: [{ type: "text", text: `Error: ${msg}` }],
					details: { path: params.path, lines: 0, truncated: false },
				};
			}
		},
	};
}
