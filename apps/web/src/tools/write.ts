import { resolve } from "node:path";
import type { AgentTool } from "../runtime/agent";
import { Type } from "../runtime/agent";
import { defaultFileOperations, type FileOperations } from "./file-operations";

// Extracted so the return type can reference `typeof parameters` instead of `AgentTool<any>`.
const parameters = Type.Object({
	path: Type.String({
		description: "Path to the file (relative to cwd or absolute)",
	}),
	content: Type.String({ description: "Content to write" }),
});

export function createWriteTool(
	cwd: string,
	files: FileOperations = defaultFileOperations,
): AgentTool<typeof parameters> {
	return {
		name: "write",
		label: "Write",
		description:
			"Write content to a file. Creates the file and any missing parent directories.",
		parameters,
		async execute(_id, params, _signal) {
			try {
				const abs = resolve(cwd, params.path);
				await files.write(abs, params.content);
				const lines = params.content.split("\n").length;
				return {
					content: [{ type: "text", text: `Wrote ${lines} lines to ${abs}` }],
					details: { path: abs, lines },
				};
			} catch (err) {
				const msg = err instanceof Error ? err.message : String(err);
				return {
					content: [{ type: "text", text: `Error: ${msg}` }],
					details: { path: params.path, lines: 0 },
				};
			}
		},
	};
}
