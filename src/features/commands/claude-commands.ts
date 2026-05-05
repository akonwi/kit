/**
 * Claude Code Commands Compatibility
 *
 * Discovers `.claude/commands/*.md` in the project root and returns them
 * as Command objects with a `cc:` prefix. For example,
 * `.claude/commands/draft-pr.md` becomes `/cc:draft-pr`.
 *
 * Command files use frontmatter (`description`, `argument-hint`) and
 * `$ARGUMENTS` / `$@` for argument substitution.
 */

import { existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { parseCommandArgs, substituteArgs } from "../prompts/substitute";
import type { Command } from "./types";

interface ClaudeCommandMeta {
	name: string;
	filePath: string;
	description: string;
	argName?: string;
}

function parseFrontmatter(content: string): {
	attributes: Record<string, string>;
	body: string;
} {
	const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/);
	if (!match) return { attributes: {}, body: content };

	const attributes: Record<string, string> = {};
	for (const line of match[1].split("\n")) {
		const sep = line.indexOf(":");
		if (sep === -1) continue;
		const key = line.slice(0, sep).trim();
		const value = line.slice(sep + 1).trim();
		if (key && value) attributes[key] = value;
	}

	return { attributes, body: match[2] };
}

export function discoverClaudeCommandFiles(cwd: string): ClaudeCommandMeta[] {
	const commandsDir = path.join(cwd, ".claude", "commands");
	if (!existsSync(commandsDir)) return [];

	const commands: ClaudeCommandMeta[] = [];

	let entries: import("node:fs").Dirent[];
	try {
		entries = readdirSync(commandsDir, { withFileTypes: true });
	} catch {
		return [];
	}

	for (const entry of entries) {
		if (!entry.isFile() || !entry.name.endsWith(".md")) continue;

		const name = entry.name.replace(/\.md$/, "");
		const filePath = path.join(commandsDir, entry.name);

		try {
			const raw = readFileSync(filePath, "utf8");
			const { attributes, body } = parseFrontmatter(raw);

			const description =
				attributes.description ||
				body
					.split("\n")
					.find((l) => l.trim().length > 0)
					?.trim()
					.slice(0, 80) ||
				name;
			const argName = attributes["argument-hint"]?.trim() || undefined;

			commands.push({ name, filePath, description, argName });
		} catch {
			// Skip unreadable files
		}
	}

	return commands.sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Discover `.claude/commands/*.md` and return Command objects.
 * Each command re-reads its file at invocation time so edits are
 * picked up without restart.
 */
export function discoverClaudeCommands(cwd: string): Command[] {
	const metas = discoverClaudeCommandFiles(cwd);

	return metas.map(
		(meta): Command => ({
			name: `cc:${meta.name}`,
			description: meta.description,
			...(meta.argName ? { argName: meta.argName } : {}),
			execute({ runtime, args }) {
				let raw: string;
				try {
					raw = readFileSync(meta.filePath, "utf8");
				} catch {
					return;
				}

				const { body } = parseFrontmatter(raw);
				const parsedArgs = parseCommandArgs(args);
				const prompt = substituteArgs(body, parsedArgs).trim();
				if (prompt) {
					void runtime.submitMessage(prompt);
				}
			},
		}),
	);
}
