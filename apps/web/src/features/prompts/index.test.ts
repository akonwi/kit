import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import type {
	InternalPluginAPI,
	InternalPluginCommandOptions,
} from "../../plugins";
import { PromptsPlugin } from ".";

describe("PromptsPlugin", () => {
	const tempDirs: string[] = [];

	afterEach(async () => {
		await Promise.all(
			tempDirs
				.splice(0)
				.map((directory) => rm(directory, { recursive: true, force: true })),
		);
	});

	test("schedules prompt commands remotely and refreshes project templates", async () => {
		const firstRoot = await mkdtemp(path.join(tmpdir(), "kit-prompts-first-"));
		const secondRoot = await mkdtemp(
			path.join(tmpdir(), "kit-prompts-second-"),
		);
		tempDirs.push(firstRoot, secondRoot);
		const firstName = `first-${path.basename(firstRoot)}`;
		const secondName = `second-${path.basename(secondRoot)}`;
		const firstDirectory = path.join(firstRoot, ".agents", "prompts");
		const secondDirectory = path.join(secondRoot, ".agents", "prompts");
		await mkdir(firstDirectory, { recursive: true });
		await mkdir(secondDirectory, { recursive: true });
		await writeFile(
			path.join(firstDirectory, `${firstName}.md`),
			[
				"---",
				"description: Review a module",
				"---",
				"Review $1 carefully. Extra context: $@",
			].join("\n"),
		);
		await writeFile(
			path.join(secondDirectory, `${secondName}.md`),
			"Summarize $@",
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
		const dispose = PromptsPlugin({
			system: { cwd: firstRoot },
			registerCommand,
			addDebugSection: () => () => {},
			on: (_type: string, handler: (event: { cwd: string }) => void) => {
				cwdHandler = handler;
				return () => {};
			},
		} as unknown as InternalPluginAPI);

		const firstCommand = commands.get(firstName);
		expect(firstCommand).toMatchObject({
			description: "Review a module",
			argName: "args",
		});
		expect(typeof firstCommand?.executeTransportNeutral).toBe("function");
		let scheduled: [string, string, string] | undefined;
		await firstCommand?.executeTransportNeutral?.({
			args: '"auth module" thoroughly',
			schedulePromptCommand: (command, args, expandedPrompt) => {
				scheduled = [command, args, expandedPrompt];
			},
		});
		expect(scheduled).toEqual([
			firstName,
			'"auth module" thoroughly',
			"Review auth module carefully. Extra context: auth module thoroughly",
		]);

		if (!cwdHandler) throw new Error("Expected cwd subscription");
		cwdHandler({ cwd: secondRoot });
		expect(commands.has(firstName)).toBe(false);
		expect(commands.has(secondName)).toBe(true);
		dispose();
		expect(commands.has(secondName)).toBe(false);
	});
});
