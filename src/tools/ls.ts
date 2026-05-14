import { readdir, stat } from "node:fs/promises";
import { join, resolve } from "node:path";
import type { AgentTool } from "@earendil-works/pi-agent-core";
import { Type } from "@earendil-works/pi-ai";

// Extracted so the return type can reference `typeof parameters` instead of `AgentTool<any>`.
const parameters = Type.Object({
	path: Type.Optional(
		Type.String({ description: "Directory to list (default: cwd)" }),
	),
});

export function createLsTool(cwd: string): AgentTool<typeof parameters> {
	return {
		name: "ls",
		label: "LS",
		description: "List files and directories at a path.",
		parameters,
		async execute(_id, params, _signal) {
			try {
				const target = resolve(cwd, params.path ?? ".");
				const entries = await readdir(target);
				const lines: string[] = [];

				for (const entry of entries.sort()) {
					try {
						const info = await stat(join(target, entry));
						lines.push(info.isDirectory() ? `${entry}/` : entry);
					} catch {
						lines.push(entry);
					}
				}

				return {
					content: [{ type: "text", text: lines.join("\n") || "(empty)" }],
					details: { path: target, count: lines.length },
				};
			} catch (err) {
				const msg = err instanceof Error ? err.message : String(err);
				return {
					content: [{ type: "text", text: `Error: ${msg}` }],
					details: { path: params.path ?? ".", count: 0 },
				};
			}
		},
	};
}
