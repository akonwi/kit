import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";
import { getKitPaths } from "../../paths";
import { parseFrontmatter } from "../skills/frontmatter";

export type SubagentSource =
	| "kit-user"
	| "kit-project"
	| "pi-user"
	| "pi-project";

export interface SubagentDefinition {
	name: string;
	description: string;
	model?: string;
	instructions: string;
	filePath: string;
	baseDir: string;
	source: SubagentSource;
}

export interface LoadSubagentsOptions {
	homeDir?: string;
	kitRoot?: string;
}

export interface LoadSubagentsResult {
	agents: SubagentDefinition[];
	warnings: string[];
}

function resolveFileEntries(dir: string): import("node:fs").Dirent[] {
	try {
		return readdirSync(dir, { withFileTypes: true })
			.filter((entry) => entry.name.endsWith(".md"))
			.sort((a, b) => a.name.localeCompare(b.name));
	} catch {
		return [];
	}
}

function isReadableFile(dir: string, entry: import("node:fs").Dirent): boolean {
	if (entry.isFile()) return true;
	if (!entry.isSymbolicLink()) return false;
	try {
		return statSync(path.join(dir, entry.name)).isFile();
	} catch {
		return false;
	}
}

function loadSubagentFromFile(
	filePath: string,
	source: SubagentSource,
): { agent: SubagentDefinition | null; warnings: string[] } {
	const warnings: string[] = [];

	try {
		const rawContent = readFileSync(filePath, "utf8");
		const { frontmatter, body } = parseFrontmatter(rawContent);
		const name =
			typeof frontmatter.name === "string" ? frontmatter.name.trim() : "";
		const description =
			typeof frontmatter.description === "string"
				? frontmatter.description.trim()
				: "";
		const model =
			typeof frontmatter.model === "string" && frontmatter.model.trim()
				? frontmatter.model.trim()
				: undefined;

		if (!name) {
			warnings.push(`${filePath}: name is required`);
			return { agent: null, warnings };
		}
		if (!description) {
			warnings.push(`${filePath}: description is required`);
			return { agent: null, warnings };
		}

		return {
			agent: {
				name,
				description,
				model,
				instructions: body.trim(),
				filePath,
				baseDir: path.dirname(filePath),
				source,
			},
			warnings,
		};
	} catch (error) {
		warnings.push(
			`${filePath}: ${error instanceof Error ? error.message : "failed to load"}`,
		);
		return { agent: null, warnings };
	}
}

function loadSubagentsFromDir(
	dir: string,
	source: SubagentSource,
): { agents: SubagentDefinition[]; warnings: string[] } {
	const agents: SubagentDefinition[] = [];
	const warnings: string[] = [];
	if (!existsSync(dir)) return { agents, warnings };

	for (const entry of resolveFileEntries(dir)) {
		if (!isReadableFile(dir, entry)) continue;
		const filePath = path.join(dir, entry.name);
		const result = loadSubagentFromFile(filePath, source);
		if (result.agent) agents.push(result.agent);
		warnings.push(...result.warnings);
	}

	return { agents, warnings };
}

export function loadSubagents(
	cwd: string,
	options: LoadSubagentsOptions = {},
): LoadSubagentsResult {
	const homeDir = options.homeDir ?? homedir();
	const kitRoot = options.kitRoot ?? getKitPaths(homeDir).kitRoot;
	const allAgents: SubagentDefinition[] = [];
	const warnings: string[] = [];
	const seenNames = new Set<string>();

	const dirs: Array<{ dir: string; source: SubagentSource }> = [
		{ dir: path.join(kitRoot, "agents"), source: "kit-user" },
		{ dir: path.resolve(cwd, ".kit", "agents"), source: "kit-project" },
		{ dir: path.join(homeDir, ".pi", "agent", "agents"), source: "pi-user" },
		{ dir: path.resolve(cwd, ".pi", "agents"), source: "pi-project" },
	];

	for (const { dir, source } of dirs) {
		const result = loadSubagentsFromDir(dir, source);
		warnings.push(...result.warnings);
		for (const agent of result.agents) {
			if (seenNames.has(agent.name)) {
				warnings.push(
					`${agent.filePath}: name "${agent.name}" already loaded, skipping`,
				);
				continue;
			}
			seenNames.add(agent.name);
			allAgents.push(agent);
		}
	}

	return { agents: allAgents, warnings };
}
