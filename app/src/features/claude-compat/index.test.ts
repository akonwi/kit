import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import type {
	InternalPluginAPI,
	InternalPluginCommandOptions,
} from "../../plugins";
import { ClaudeCompatibilityPlugin } from ".";

describe("ClaudeCompatibilityPlugin", () => {
	const tempDirs: string[] = [];

	afterEach(async () => {
		await Promise.all(
			tempDirs
				.splice(0)
				.map((directory) => rm(directory, { recursive: true, force: true })),
		);
	});

	test("registers Claude commands for transport-neutral prompt scheduling", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-claude-plugin-"));
		tempDirs.push(root);
		const commandsDirectory = path.join(root, ".claude", "commands");
		await mkdir(commandsDirectory, { recursive: true });
		await writeFile(
			path.join(commandsDirectory, "review.md"),
			[
				"---",
				"description: Review a module",
				"argument-hint: module",
				"---",
				"Review $1 carefully. Extra context: $@",
			].join("\n"),
		);

		let commandId = "";
		let commandOptions: InternalPluginCommandOptions | undefined;
		const registerCommand: InternalPluginAPI["registerCommand"] = (
			id,
			options,
		) => {
			commandId = id;
			commandOptions = options;
			return () => {};
		};
		ClaudeCompatibilityPlugin({
			system: { cwd: root },
			registerCommand,
			addDebugSection: () => () => {},
			on: () => () => {},
		} as unknown as InternalPluginAPI);

		expect(commandId).toBe("cc:review");
		expect(commandOptions).toMatchObject({
			description: "Review a module",
			argName: "module",
		});
		expect(typeof commandOptions?.executeTransportNeutral).toBe("function");
		await writeFile(
			path.join(commandsDirectory, "review.md"),
			"This post-discovery edit must not change an in-flight remote command.",
		);

		let scheduled: [string, string, string] | undefined;
		await commandOptions?.executeTransportNeutral?.({
			args: '"auth module" thoroughly',
			schedulePromptCommand: (command, args, expandedPrompt) => {
				scheduled = [command, args, expandedPrompt];
			},
		});
		expect(scheduled).toEqual([
			"cc:review",
			'"auth module" thoroughly',
			"Review auth module carefully. Extra context: auth module thoroughly",
		]);
	});

	test("replaces workspace commands when the session cwd changes", async () => {
		const firstRoot = await mkdtemp(path.join(tmpdir(), "kit-claude-first-"));
		const secondRoot = await mkdtemp(path.join(tmpdir(), "kit-claude-second-"));
		tempDirs.push(firstRoot, secondRoot);
		await mkdir(path.join(firstRoot, ".claude", "commands"), {
			recursive: true,
		});
		await mkdir(path.join(secondRoot, ".claude", "commands"), {
			recursive: true,
		});
		await writeFile(
			path.join(firstRoot, ".claude", "commands", "first.md"),
			"First command",
		);
		await writeFile(
			path.join(secondRoot, ".claude", "commands", "second.md"),
			"Second command",
		);

		const commands = new Map<string, InternalPluginCommandOptions>();
		let cwdHandler: ((event: { cwd: string }) => void) | undefined;
		const registerCommand: InternalPluginAPI["registerCommand"] = (
			id,
			options,
		) => {
			commands.set(id, options);
			return () => {
				if (commands.get(id) === options) commands.delete(id);
			};
		};
		const dispose = ClaudeCompatibilityPlugin({
			system: { cwd: firstRoot },
			registerCommand,
			addDebugSection: () => () => {},
			on: (_type: string, handler: (event: { cwd: string }) => void) => {
				cwdHandler = handler;
				return () => {};
			},
		} as unknown as InternalPluginAPI);

		expect([...commands.keys()]).toEqual(["cc:first"]);
		if (!cwdHandler) throw new Error("Expected cwd subscription");
		cwdHandler({ cwd: secondRoot });
		expect([...commands.keys()]).toEqual(["cc:second"]);
		dispose();
		expect(commands.size).toBe(0);
	});
});
