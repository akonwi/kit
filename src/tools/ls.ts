import { readdir, stat } from "node:fs/promises";
import { join, resolve } from "node:path";
import { Type } from "@mariozechner/pi-ai";
import type { AgentTool } from "@mariozechner/pi-agent-core";

export function createLsTool(cwd: string): AgentTool {
  return {
    name: "ls",
    label: "LS",
    description: "List files and directories at a path.",
    parameters: Type.Object({
      path: Type.Optional(Type.String({ description: "Directory to list (default: cwd)" })),
    }),
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
          isError: true,
        } as any;
      }
    },
  };
}
