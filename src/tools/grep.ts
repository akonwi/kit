import { createInterface } from "node:readline";
import { readFile, stat } from "node:fs/promises";
import { basename, relative } from "node:path";
import { spawn } from "node:child_process";
import { resolve } from "node:path";
import { Type } from "@mariozechner/pi-ai";
import type { AgentTool } from "@mariozechner/pi-agent-core";

const DEFAULT_LIMIT = 100;
const MAX_LINE_LENGTH = 2000;
const MAX_BYTES = 200 * 1024; // 200KB

export function createGrepTool(cwd: string): AgentTool<any> {
  return {
    name: "grep",
    label: "Grep",
    description: `Search file contents for a pattern. Returns matching lines with file paths and line numbers. Respects .gitignore. Output truncated to ${DEFAULT_LIMIT} matches. Long lines truncated to ${MAX_LINE_LENGTH} chars.`,
    parameters: Type.Object({
      pattern: Type.String({ description: "Search pattern (regex or literal string)" }),
      path: Type.Optional(Type.String({ description: "Directory or file to search (default: cwd)" })),
      glob: Type.Optional(Type.String({ description: "Filter files by glob pattern, e.g. '*.ts'" })),
      ignoreCase: Type.Optional(Type.Boolean({ description: "Case-insensitive search (default: false)" })),
      literal: Type.Optional(Type.Boolean({ description: "Treat pattern as literal string instead of regex (default: false)" })),
      context: Type.Optional(Type.Number({ description: "Lines of context before and after each match (default: 0)" })),
      limit: Type.Optional(Type.Number({ description: `Max matches to return (default: ${DEFAULT_LIMIT})` })),
    }),

    async execute(_id, params, signal) {
      return new Promise((resolve_, reject) => {
        if (signal?.aborted) { reject(new Error("Aborted")); return; }

        const searchPath = resolve(cwd, params.path || ".");
        const effectiveLimit = Math.max(1, params.limit ?? DEFAULT_LIMIT);
        const contextLines = Math.max(0, params.context ?? 0);

        (async () => {
          let isDir = true;
          try {
            isDir = (await stat(searchPath)).isDirectory();
          } catch {
            reject(new Error(`Path not found: ${searchPath}`));
            return;
          }

          const formatPath = (filePath: string) => {
            if (isDir) {
              const rel = relative(searchPath, filePath);
              if (rel && !rel.startsWith("..")) return rel.replace(/\\/g, "/");
            }
            return basename(filePath);
          };

          // Cache file lines for context rendering
          const fileCache = new Map<string, string[]>();
          const getLines = async (filePath: string): Promise<string[]> => {
            let lines = fileCache.get(filePath);
            if (!lines) {
              try {
                const content = await readFile(filePath, "utf8");
                lines = content.replace(/\r\n/g, "\n").replace(/\r/g, "\n").split("\n");
              } catch {
                lines = [];
              }
              fileCache.set(filePath, lines);
            }
            return lines;
          };

          const truncateLine = (s: string): { text: string; wasTruncated: boolean } => {
            if (s.length <= MAX_LINE_LENGTH) return { text: s, wasTruncated: false };
            return { text: s.slice(0, MAX_LINE_LENGTH) + "…", wasTruncated: true };
          };

          const args: string[] = ["--json", "--line-number", "--color=never", "--hidden"];
          if (params.ignoreCase) args.push("--ignore-case");
          if (params.literal) args.push("--fixed-strings");
          if (params.glob) args.push("--glob", params.glob);
          args.push(params.pattern, searchPath);

          const child = spawn("rg", args, { stdio: ["ignore", "pipe", "pipe"] });
          const rl = createInterface({ input: child.stdout });

          let matchCount = 0;
          let matchLimitReached = false;
          let linesTruncated = false;
          let outputBytes = 0;
          let bytesTruncated = false;
          let aborted = false;
          let killed = false;

          const matches: Array<{ filePath: string; lineNumber: number }> = [];
          const outputLines: string[] = [];

          const stopChild = (dueToLimit = false) => {
            if (!child.killed) { killed = dueToLimit; child.kill(); }
          };

          const onAbort = () => { aborted = true; stopChild(); };
          signal?.addEventListener("abort", onAbort, { once: true });

          rl.on("line", (line) => {
            if (!line.trim() || matchCount >= effectiveLimit) return;
            let event: any;
            try { event = JSON.parse(line); } catch { return; }
            if (event.type === "match") {
              const filePath = event.data?.path?.text;
              const lineNumber = event.data?.line_number;
              if (filePath && typeof lineNumber === "number") {
                matchCount++;
                matches.push({ filePath, lineNumber });
              }
              if (matchCount >= effectiveLimit) {
                matchLimitReached = true;
                stopChild(true);
              }
            }
          });

          child.on("error", (err) => {
            signal?.removeEventListener("abort", onAbort);
            rl.close();
            reject(new Error(`rg not available: ${err.message}. Install ripgrep.`));
          });

          child.on("close", async (code) => {
            signal?.removeEventListener("abort", onAbort);
            rl.close();

            if (aborted) { reject(new Error("Aborted")); return; }
            if (!killed && code !== 0 && code !== 1) {
              reject(new Error(`rg exited with code ${code}`));
              return;
            }
            if (matchCount === 0) {
              resolve_({ content: [{ type: "text", text: "No matches found" }], details: {} });
              return;
            }

            // Format matches after streaming (context requires async file reads)
            for (const { filePath, lineNumber } of matches) {
              const relPath = formatPath(filePath);
              const lines = await getLines(filePath);
              const start = contextLines > 0 ? Math.max(1, lineNumber - contextLines) : lineNumber;
              const end = contextLines > 0 ? Math.min(lines.length, lineNumber + contextLines) : lineNumber;

              for (let cur = start; cur <= end; cur++) {
                const raw = (lines[cur - 1] ?? "").replace(/\r/g, "");
                const { text, wasTruncated } = truncateLine(raw);
                if (wasTruncated) linesTruncated = true;
                const sep = cur === lineNumber ? ":" : "-";
                const formatted = `${relPath}:${cur}${sep} ${text}`;
                outputBytes += formatted.length;
                if (outputBytes > MAX_BYTES) { bytesTruncated = true; break; }
                outputLines.push(formatted);
              }
              if (bytesTruncated) break;
            }

            let output = outputLines.join("\n");
            const notices: string[] = [];
            if (matchLimitReached) notices.push(`${effectiveLimit} match limit reached — use limit=${effectiveLimit * 2} for more or refine pattern`);
            if (bytesTruncated) notices.push(`${MAX_BYTES / 1024}KB output limit reached`);
            if (linesTruncated) notices.push(`some lines truncated to ${MAX_LINE_LENGTH} chars — use read tool for full lines`);
            if (notices.length > 0) output += `\n\n[${notices.join(". ")}]`;

            resolve_({
              content: [{ type: "text", text: output }],
              details: { matchCount, matchLimitReached, bytesTruncated, linesTruncated },
            });
          });
        })();
      });
    },
  };
}
