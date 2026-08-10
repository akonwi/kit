import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import {
	discoverClaudeCommandFiles,
	discoverClaudeCommands,
} from "./claude-commands";
import type { CommandContext } from "./types";

describe("Claude Code command discovery", () => {
	const tempDirs: string[] = [];

	afterEach(async () => {
		await Promise.all(
			tempDirs
				.splice(0)
				.map((dir) => rm(dir, { recursive: true, force: true })),
		);
	});

	test("discovers .claude/commands as /cc:* slash commands with arg hints", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-claude-commands-"));
		tempDirs.push(root);
		const commandsDir = path.join(root, ".claude", "commands");
		await mkdir(commandsDir, { recursive: true });
		await writeFile(
			path.join(commandsDir, "draft-pr.md"),
			[
				"---",
				"description: Draft a PR message",
				"argument-hint: scope",
				"---",
				"Write a pull request description for $1. Context: $@",
			].join("\n"),
		);

		const metas = discoverClaudeCommandFiles(root);
		expect(metas).toHaveLength(1);
		expect(metas[0]).toMatchObject({
			name: "draft-pr",
			description: "Draft a PR message",
			argName: "scope",
		});

		const commands = discoverClaudeCommands(root);
		expect(commands).toHaveLength(1);
		expect(commands[0]?.name).toBe("cc:draft-pr");
		expect(commands[0]?.argName).toBe("scope");
	});

	test("skips command directories outside the workspace", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-claude-safe-read-"));
		const outside = await mkdtemp(path.join(tmpdir(), "kit-claude-outside-"));
		tempDirs.push(root, outside);
		const claudeDir = path.join(root, ".claude");
		const commandsDir = path.join(claudeDir, "commands");
		await mkdir(claudeDir, { recursive: true });
		await writeFile(path.join(outside, "escaped.md"), "Outside content");
		await symlink(outside, commandsDir);

		expect(discoverClaudeCommandFiles(root)).toEqual([]);
	});

	test("substitutes Claude command arguments on execution", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-claude-exec-"));
		tempDirs.push(root);
		const commandsDir = path.join(root, ".claude", "commands");
		await mkdir(commandsDir, { recursive: true });
		await writeFile(
			path.join(commandsDir, "review.md"),
			"Review $1 carefully. Extra context: $@",
		);

		const command = discoverClaudeCommands(root)[0];
		let submitted = "";
		const runtime = {
			submitPromptCommandMessage: async (
				_command: string,
				_args: string,
				message: string,
			) => {
				submitted = message;
			},
		} as CommandContext["runtime"];

		await command?.execute({
			runtime,
			args: '"auth module" thoroughly',
			picker: {} as CommandContext["picker"],
			toast: () => {},
			attachments: {} as CommandContext["attachments"],
			reviewDrafts: {} as CommandContext["reviewDrafts"],
			reviewWorkspace: {} as CommandContext["reviewWorkspace"],
			_reload: async () => {},
			openCustomOverlay: async () => undefined as never,
		});

		expect(submitted).toBe(
			"Review auth module carefully. Extra context: auth module thoroughly",
		);
	});
});
