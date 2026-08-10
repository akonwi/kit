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

import {
	closeSync,
	constants,
	existsSync,
	lstatSync,
	openSync,
	readdirSync,
	readFileSync,
	realpathSync,
} from "node:fs";
import path from "node:path";
import { parseCommandArgs, substituteArgs } from "../prompts/substitute";
import type { Command } from "./types";

export interface ClaudeCommandMeta {
	name: string;
	filePath: string;
	description: string;
	argName?: string;
	rawContent: string;
}

function isPathInside(parent: string, candidate: string): boolean {
	const relative = path.relative(parent, candidate);
	return (
		relative.length > 0 &&
		!relative.startsWith("..") &&
		!path.isAbsolute(relative)
	);
}

function commandDirectory(cwd: string): {
	workspacePath: string;
	directoryPath: string;
} | null {
	const directory = path.join(cwd, ".claude", "commands");
	if (!existsSync(directory)) return null;
	try {
		if (!lstatSync(directory).isDirectory()) return null;
		const workspacePath = realpathSync(cwd);
		const directoryPath = realpathSync(directory);
		return isPathInside(workspacePath, directoryPath)
			? { workspacePath, directoryPath }
			: null;
	} catch {
		return null;
	}
}

function readCommandFile(
	workspacePath: string,
	commandsDirectory: string,
	filePath: string,
): string | null {
	let descriptor: number | null = null;
	try {
		if (!lstatSync(commandsDirectory).isDirectory()) return null;
		if (!lstatSync(filePath).isFile()) return null;
		const directoryPath = realpathSync(commandsDirectory);
		const commandPath = realpathSync(filePath);
		if (
			!isPathInside(workspacePath, directoryPath) ||
			!isPathInside(directoryPath, commandPath)
		) {
			return null;
		}
		descriptor = openSync(
			filePath,
			constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0),
		);
		return readFileSync(descriptor, "utf8");
	} catch {
		return null;
	} finally {
		if (descriptor !== null) closeSync(descriptor);
	}
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
	const directory = commandDirectory(cwd);
	if (!directory) return [];

	let entries: import("node:fs").Dirent[];
	try {
		entries = readdirSync(directory.directoryPath, { withFileTypes: true });
	} catch {
		return [];
	}

	const commands: ClaudeCommandMeta[] = [];
	for (const entry of entries) {
		if (!entry.isFile() || !entry.name.endsWith(".md")) continue;
		const name = entry.name.replace(/\.md$/, "");
		const filePath = path.join(directory.directoryPath, entry.name);
		const rawContent = readCommandFile(
			directory.workspacePath,
			directory.directoryPath,
			filePath,
		);
		if (rawContent === null) continue;
		const { attributes, body } = parseFrontmatter(rawContent);
		const description =
			attributes.description ||
			body
				.split("\n")
				.find((line) => line.trim().length > 0)
				?.trim()
				.slice(0, 80) ||
			name;
		const argName = attributes["argument-hint"]?.trim() || undefined;
		commands.push({ name, filePath, description, argName, rawContent });
	}

	return commands.sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Read the local command again so TUI executions pick up safe edits without a
 * plugin reload. Remote execution uses the discovery snapshot instead.
 */
export function readClaudeCommandPrompt(
	filePath: string,
	args: string,
): string | null {
	const commandsDirectory = path.dirname(filePath);
	const directory = commandDirectory(
		path.resolve(commandsDirectory, "..", ".."),
	);
	if (!directory) return null;
	const raw = readCommandFile(
		directory.workspacePath,
		directory.directoryPath,
		filePath,
	);
	return raw === null ? null : expandClaudeCommandPrompt(raw, args);
}

function expandClaudeCommandPrompt(raw: string, args: string): string | null {
	const { body } = parseFrontmatter(raw);
	const parsedArgs = parseCommandArgs(args);
	const prompt = substituteArgs(body, parsedArgs).trim();
	return prompt || null;
}

export function expandRemoteClaudeCommandPrompt(
	raw: string,
	args: string,
	signal?: AbortSignal,
): string | null {
	signal?.throwIfAborted();
	const prompt = expandClaudeCommandPrompt(raw, args);
	signal?.throwIfAborted();
	return prompt;
}

export function discoverClaudeCommands(cwd: string): Command[] {
	const metas = discoverClaudeCommandFiles(cwd);

	return metas.map(
		(meta): Command => ({
			name: `cc:${meta.name}`,
			description: meta.description,
			...(meta.argName ? { argName: meta.argName } : {}),
			execute({ runtime, args }) {
				const prompt = readClaudeCommandPrompt(meta.filePath, args);
				if (prompt) {
					void runtime.submitPromptCommandMessage(
						`cc:${meta.name}`,
						args,
						prompt,
					);
				}
			},
			executeTransportNeutral({ args, schedulePromptCommand, signal }) {
				const commandName = `cc:${meta.name}`;
				const prompt = expandRemoteClaudeCommandPrompt(
					meta.rawContent,
					args,
					signal,
				);
				if (!prompt) {
					throw new Error(
						`Claude-compatible command is empty: /${commandName}`,
					);
				}
				schedulePromptCommand(commandName, args, prompt);
			},
		}),
	);
}
