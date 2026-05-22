import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { relative, resolve } from "node:path";
import type { AgentTool } from "@earendil-works/pi-agent-core";
import { Type } from "@earendil-works/pi-ai";
import { globSync } from "glob";

const DEFAULT_LIMIT = 1000;
const MAX_BYTES = 200 * 1024; // 200KB

function toPosix(p: string): string {
	return p.split("/").join("/").replace(/\\/g, "/");
}

// Extracted so the return type can reference `typeof parameters` instead of `AgentTool<any>`.
const parameters = Type.Object({
	pattern: Type.String({
		description:
			"Glob pattern to match files, e.g. '*.ts', '**/*.json', 'src/**/*.spec.ts'",
	}),
	path: Type.Optional(
		Type.String({ description: "Directory to search (default: cwd)" }),
	),
	limit: Type.Optional(
		Type.Number({ description: `Max results (default: ${DEFAULT_LIMIT})` }),
	),
});

export function createFindTool(cwd: string): AgentTool<typeof parameters> {
	return {
		name: "find",
		label: "Find",
		description: `Find files by glob pattern. Returns paths relative to the search directory. Respects .gitignore. Truncated to ${DEFAULT_LIMIT} results.`,
		parameters,

		async execute(_id, params, signal) {
			return new Promise((resolve_, reject) => {
				if (signal?.aborted) {
					reject(new Error("Aborted"));
					return;
				}

				const onAbort = () => reject(new Error("Aborted"));
				signal?.addEventListener("abort", onAbort, { once: true });

				const cleanup = () => signal?.removeEventListener("abort", onAbort);

				try {
					const searchPath = resolve(cwd, params.path || ".");
					const effectiveLimit = params.limit ?? DEFAULT_LIMIT;

					if (!existsSync(searchPath)) {
						cleanup();
						reject(new Error(`Path not found: ${searchPath}`));
						return;
					}

					// Try fd first
					const fdResult = tryFd(params.pattern, searchPath, effectiveLimit);

					let lines: string[];
					if (fdResult !== null) {
						lines = fdResult;
					} else {
						// Fall back to glob
						lines = tryGlob(params.pattern, searchPath, effectiveLimit);
					}

					// Relativize paths
					const relativized = lines
						.map((line) => {
							const trimmed = line.replace(/\r$/, "").trim();
							if (!trimmed) return null;
							const hadSlash = trimmed.endsWith("/") || trimmed.endsWith("\\");
							let rel = trimmed.startsWith(searchPath)
								? trimmed.slice(searchPath.length + 1)
								: relative(searchPath, trimmed);
							if (hadSlash && !rel.endsWith("/")) rel += "/";
							return toPosix(rel);
						})
						.filter(Boolean) as string[];

					cleanup();

					if (relativized.length === 0) {
						resolve_({
							content: [
								{ type: "text", text: "No files found matching pattern" },
							],
							details: {},
						});
						return;
					}

					const resultLimitReached = relativized.length >= effectiveLimit;
					let output = relativized.join("\n");

					// Byte cap
					let bytesTruncated = false;
					if (output.length > MAX_BYTES) {
						output = output.slice(0, MAX_BYTES);
						// Trim to last complete line
						const lastNl = output.lastIndexOf("\n");
						if (lastNl > 0) output = output.slice(0, lastNl);
						bytesTruncated = true;
					}

					const notices: string[] = [];
					if (resultLimitReached)
						notices.push(
							`${effectiveLimit} results limit reached — use limit=${effectiveLimit * 2} or refine pattern`,
						);
					if (bytesTruncated)
						notices.push(`${MAX_BYTES / 1024}KB output limit reached`);
					if (notices.length > 0) output += `\n\n[${notices.join(". ")}]`;

					resolve_({
						content: [{ type: "text", text: output }],
						details: {
							count: relativized.length,
							resultLimitReached,
							bytesTruncated,
						},
					});
				} catch (err) {
					cleanup();
					reject(err);
				}
			});
		},
	};
}

function tryFd(
	pattern: string,
	searchPath: string,
	limit: number,
): string[] | null {
	// Collect .gitignore files
	const gitignoreArgs: string[] = [];
	const rootGitignore = `${searchPath}/.gitignore`;
	if (existsSync(rootGitignore)) {
		gitignoreArgs.push("--ignore-file", rootGitignore);
	}
	try {
		const nested = globSync("**/.gitignore", {
			cwd: searchPath,
			dot: true,
			absolute: true,
			ignore: ["**/node_modules/**", "**/.git/**"],
		});
		for (const f of nested) {
			gitignoreArgs.push("--ignore-file", f);
		}
	} catch {
		/* ignore */
	}

	const args = [
		"--glob",
		"--color=never",
		"--hidden",
		"--max-results",
		String(limit),
		...gitignoreArgs,
		pattern,
		searchPath,
	];

	const result = spawnSync("fd", args, {
		encoding: "utf-8",
		maxBuffer: 10 * 1024 * 1024,
	});
	if (result.error) return null; // fd not found
	if (result.status !== 0 && !result.stdout?.trim()) return null;
	return (result.stdout ?? "").trim().split("\n").filter(Boolean);
}

function tryGlob(pattern: string, searchPath: string, limit: number): string[] {
	const results = globSync(pattern, {
		cwd: searchPath,
		dot: true,
		absolute: true,
		ignore: ["**/node_modules/**", "**/.git/**", "**/dist/**", "**/build/**"],
	});
	return results.slice(0, limit);
}
