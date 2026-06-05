/**
 * Prompt template discovery and loading.
 *
 * Each .md file in a prompts directory becomes a slash command.
 * The filename (without .md) is the command name.
 * Optional YAML frontmatter provides a description.
 * The body is the template content with argument placeholders.
 *
 * Search locations:
 * - ~/.kit/prompts/       (user-global)
 * - .agents/prompts/      (project-local, relative to cwd)
 */

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { getKitPaths } from "../../paths";
import { parseFrontmatter } from "../skills/frontmatter";

export interface PromptTemplate {
	/** Command name (filename without .md) */
	name: string;
	description: string;
	/** Template body with argument placeholders */
	content: string;
	filePath: string;
	source: "user" | "project";
}

function loadTemplateFromFile(
	filePath: string,
	source: PromptTemplate["source"],
): PromptTemplate | null {
	try {
		const rawContent = readFileSync(filePath, "utf8");
		const { frontmatter, body } = parseFrontmatter(rawContent);

		const name = path.basename(filePath).replace(/\.md$/, "");

		let description =
			typeof frontmatter.description === "string"
				? frontmatter.description
				: "";
		if (!description) {
			const firstLine = body.split("\n").find((line) => line.trim());
			if (firstLine) {
				description =
					firstLine.length > 60 ? `${firstLine.slice(0, 60)}...` : firstLine;
			}
		}

		return { name, description, content: body, filePath, source };
	} catch {
		return null;
	}
}

function scanDir(
	dir: string,
	source: PromptTemplate["source"],
): PromptTemplate[] {
	const templates: PromptTemplate[] = [];
	if (!existsSync(dir)) return templates;

	try {
		const entries = readdirSync(dir, { withFileTypes: true });
		for (const entry of entries) {
			const fullPath = path.join(dir, entry.name);

			let isFile = entry.isFile();
			if (entry.isSymbolicLink()) {
				try {
					isFile = statSync(fullPath).isFile();
				} catch {
					continue;
				}
			}

			if (isFile && entry.name.endsWith(".md")) {
				const template = loadTemplateFromFile(fullPath, source);
				if (template) templates.push(template);
			}
		}
	} catch {
		// Ignore unreadable directories
	}

	return templates;
}

/**
 * Load prompt templates from all configured locations.
 * First-loaded wins on name collisions (user > project).
 */
export function loadPromptTemplates(cwd: string): PromptTemplate[] {
	const kitPaths = getKitPaths();
	const templates: PromptTemplate[] = [];
	const seenNames = new Set<string>();

	const dirs: Array<{ dir: string; source: PromptTemplate["source"] }> = [
		{ dir: path.join(kitPaths.kitRoot, "prompts"), source: "user" },
		{ dir: path.resolve(cwd, ".agents", "prompts"), source: "project" },
	];

	for (const { dir, source } of dirs) {
		for (const template of scanDir(dir, source)) {
			if (seenNames.has(template.name)) continue;
			seenNames.add(template.name);
			templates.push(template);
		}
	}

	return templates;
}
