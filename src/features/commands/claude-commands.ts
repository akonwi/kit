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
import type { Command } from "./types";

interface ClaudeCommandMeta {
	name: string;
	filePath: string;
	description: string;
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

function discoverCommandFiles(cwd: string): ClaudeCommandMeta[] {
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

			commands.push({ name, filePath, description });
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
	const metas = discoverCommandFiles(cwd);

	return metas.map(
		(meta): Command => ({
			name: `cc:${meta.name}`,
			description: meta.description,
			execute({ runtime }) {
				let raw: string;
				try {
					raw = readFileSync(meta.filePath, "utf8");
				} catch {
					return;
				}

				const { body } = parseFrontmatter(raw);
				// $ARGUMENTS and $@ are not substituted here — the user sends
				// the command without args via the slash picker, so we send the
				// body as-is. If arg support is needed later, the palette could
				// prompt for input first.
				const prompt = body.trim();
				if (prompt) {
					runtime.submitUserMessage(prompt);
				}
			},
		}),
	);
}
