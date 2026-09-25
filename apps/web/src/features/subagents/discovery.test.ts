import { afterEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { loadSubagents } from "./discovery";

async function writeAgent(
	root: string,
	relativePath: string,
	content: string,
): Promise<void> {
	const filePath = path.join(root, relativePath);
	await mkdir(path.dirname(filePath), { recursive: true });
	await writeFile(filePath, content);
}

describe("sub-agent discovery", () => {
	const tempDirs: string[] = [];

	afterEach(async () => {
		await Promise.all(
			tempDirs
				.splice(0)
				.map((dir) => rm(dir, { recursive: true, force: true })),
		);
	});

	test("discovers markdown sub-agents from Kit and Pi-compatible locations", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-subagents-"));
		tempDirs.push(root);
		const homeDir = path.join(root, "home");
		const cwd = path.join(root, "project");
		await mkdir(homeDir, { recursive: true });
		await mkdir(cwd, { recursive: true });

		await writeAgent(
			homeDir,
			".kit/agents/scout.md",
			[
				"---",
				"name: scout",
				"description: Fast recon",
				"model: claude-haiku-4-5",
				"---",
				"Scout instructions",
			].join("\n"),
		);
		await writeAgent(
			cwd,
			".kit/agents/reviewer.md",
			[
				"---",
				"name: reviewer",
				"description: Review for correctness",
				"---",
				"Reviewer instructions",
			].join("\n"),
		);
		const { agents, warnings } = loadSubagents(cwd, { homeDir });
		expect(warnings).toEqual([]);
		expect(agents.map((agent) => agent.name)).toEqual(["scout", "reviewer"]);
		expect(agents.map((agent) => agent.source)).toEqual([
			"kit-user",
			"kit-project",
		]);
		expect(agents[0]).toMatchObject({
			model: "claude-haiku-4-5",
			instructions: "Scout instructions",
		});
	});

	test("uses first-loaded precedence on duplicate names", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-subagents-dupe-"));
		tempDirs.push(root);
		const homeDir = path.join(root, "home");
		const cwd = path.join(root, "project");
		await mkdir(homeDir, { recursive: true });
		await mkdir(cwd, { recursive: true });

		await writeAgent(
			homeDir,
			".kit/agents/scout.md",
			[
				"---",
				"name: scout",
				"description: User scout",
				"---",
				"User instructions",
			].join("\n"),
		);
		await writeAgent(
			cwd,
			".kit/agents/scout.md",
			[
				"---",
				"name: scout",
				"description: Project scout",
				"---",
				"Project instructions",
			].join("\n"),
		);

		const { agents, warnings } = loadSubagents(cwd, { homeDir });
		expect(agents).toHaveLength(1);
		expect(agents[0]).toMatchObject({
			name: "scout",
			description: "User scout",
			instructions: "User instructions",
			source: "kit-user",
		});
		expect(
			warnings.some((warning) =>
				warning.includes('name "scout" already loaded, skipping'),
			),
		).toBe(true);
	});

	test("skips files missing required frontmatter", async () => {
		const root = await mkdtemp(path.join(tmpdir(), "kit-subagents-invalid-"));
		tempDirs.push(root);
		const homeDir = path.join(root, "home");
		const cwd = path.join(root, "project");
		await mkdir(homeDir, { recursive: true });
		await mkdir(cwd, { recursive: true });

		await writeAgent(
			homeDir,
			".kit/agents/no-description.md",
			["---", "name: scout", "---", "Instructions"].join("\n"),
		);

		const { agents, warnings } = loadSubagents(cwd, { homeDir });
		expect(agents).toEqual([]);
		expect(
			warnings.some((warning) => warning.includes("description is required")),
		).toBe(true);
	});
});
