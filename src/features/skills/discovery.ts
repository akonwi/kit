/**
 * Skill discovery and loading.
 *
 * Discovery rules (per Agent Skills spec):
 * - If a directory contains SKILL.md, treat it as a skill root (don't recurse further)
 * - Otherwise recurse into subdirectories to find SKILL.md
 * - Respects .gitignore / .ignore
 * - Skips node_modules and dotfiles
 *
 * Search locations:
 * - ~/.kit/skills/       (user-global)
 * - .agents/skills/      (project-local, relative to cwd)
 * - ~/.pi/agent/skills/  (Pi compat)
 * - ~/.claude/skills/    (Claude compat, user-global)
 * - .claude/skills/      (Claude compat, project-local)
 */

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";
import { getKitPaths } from "../../paths";
import { parseFrontmatter } from "./frontmatter";

const MAX_NAME_LENGTH = 64;
const MAX_DESCRIPTION_LENGTH = 1024;

export interface Skill {
	name: string;
	description: string;
	filePath: string;
	baseDir: string;
	source: "user" | "project" | "pi-compat" | "claude-compat";
	disableModelInvocation: boolean;
}

export interface LoadSkillsResult {
	skills: Skill[];
	warnings: string[];
}

function validateName(name: string, parentDirName: string): string[] {
	const errors: string[] = [];
	if (name !== parentDirName) {
		errors.push(
			`name "${name}" does not match parent directory "${parentDirName}"`,
		);
	}
	if (name.length > MAX_NAME_LENGTH) {
		errors.push(`name exceeds ${MAX_NAME_LENGTH} characters`);
	}
	if (!/^[a-z0-9-]+$/.test(name)) {
		errors.push("name must be lowercase a-z, 0-9, hyphens only");
	}
	if (name.startsWith("-") || name.endsWith("-")) {
		errors.push("name must not start or end with a hyphen");
	}
	if (name.includes("--")) {
		errors.push("name must not contain consecutive hyphens");
	}
	return errors;
}

function loadSkillFromFile(
	filePath: string,
	source: Skill["source"],
): { skill: Skill | null; warnings: string[] } {
	const warnings: string[] = [];

	try {
		const rawContent = readFileSync(filePath, "utf8");
		const { frontmatter } = parseFrontmatter(rawContent);
		const skillDir = path.dirname(filePath);
		const parentDirName = path.basename(skillDir);

		const name =
			typeof frontmatter.name === "string" && frontmatter.name
				? frontmatter.name
				: parentDirName;

		const description =
			typeof frontmatter.description === "string"
				? frontmatter.description
				: "";

		// Validate
		for (const err of validateName(name, parentDirName)) {
			warnings.push(`${filePath}: ${err}`);
		}
		if (!description.trim()) {
			warnings.push(`${filePath}: description is required`);
			return { skill: null, warnings };
		}
		if (description.length > MAX_DESCRIPTION_LENGTH) {
			warnings.push(
				`${filePath}: description exceeds ${MAX_DESCRIPTION_LENGTH} characters`,
			);
		}

		return {
			skill: {
				name,
				description,
				filePath,
				baseDir: skillDir,
				source,
				disableModelInvocation:
					frontmatter["disable-model-invocation"] === true,
			},
			warnings,
		};
	} catch (err) {
		warnings.push(
			`${filePath}: ${err instanceof Error ? err.message : "failed to load"}`,
		);
		return { skill: null, warnings };
	}
}

function scanDir(
	dir: string,
	source: Skill["source"],
): { skills: Skill[]; warnings: string[] } {
	const skills: Skill[] = [];
	const warnings: string[] = [];

	if (!existsSync(dir)) return { skills, warnings };

	try {
		const entries = readdirSync(dir, { withFileTypes: true });

		// First pass: check if this directory itself has a SKILL.md
		for (const entry of entries) {
			if (entry.name !== "SKILL.md") continue;

			const fullPath = path.join(dir, entry.name);
			let isFile = entry.isFile();
			if (entry.isSymbolicLink()) {
				try {
					isFile = statSync(fullPath).isFile();
				} catch {
					continue;
				}
			}
			if (!isFile) continue;

			const result = loadSkillFromFile(fullPath, source);
			if (result.skill) skills.push(result.skill);
			warnings.push(...result.warnings);
			// SKILL.md found — this is a skill root, don't recurse
			return { skills, warnings };
		}

		// Second pass: recurse into subdirectories
		for (const entry of entries) {
			if (entry.name.startsWith(".")) continue;
			if (entry.name === "node_modules") continue;

			const fullPath = path.join(dir, entry.name);
			let isDirectory = entry.isDirectory();
			if (entry.isSymbolicLink()) {
				try {
					isDirectory = statSync(fullPath).isDirectory();
				} catch {
					continue;
				}
			}
			if (!isDirectory) continue;

			const sub = scanDir(fullPath, source);
			skills.push(...sub.skills);
			warnings.push(...sub.warnings);
		}
	} catch {
		// Ignore unreadable directories
	}

	return { skills, warnings };
}

/**
 * Load skills from all configured locations.
 * First-loaded wins on name collisions (user > project > pi-compat).
 */
export function loadSkills(cwd: string): LoadSkillsResult {
	const kitPaths = getKitPaths();
	const allSkills: Skill[] = [];
	const allWarnings: string[] = [];
	const seenNames = new Set<string>();

	const dirs: Array<{ dir: string; source: Skill["source"] }> = [
		{ dir: path.join(kitPaths.kitRoot, "skills"), source: "user" },
		{ dir: path.resolve(cwd, ".agents", "skills"), source: "project" },
		{
			dir: path.join(homedir(), ".pi", "agent", "skills"),
			source: "pi-compat",
		},
		{
			dir: path.join(homedir(), ".claude", "skills"),
			source: "claude-compat",
		},
		{
			dir: path.resolve(cwd, ".claude", "skills"),
			source: "claude-compat",
		},
	];

	for (const { dir, source } of dirs) {
		const { skills, warnings } = scanDir(dir, source);
		allWarnings.push(...warnings);
		for (const skill of skills) {
			if (seenNames.has(skill.name)) {
				allWarnings.push(
					`${skill.filePath}: name "${skill.name}" already loaded, skipping`,
				);
				continue;
			}
			seenNames.add(skill.name);
			allSkills.push(skill);
		}
	}

	return { skills: allSkills, warnings: allWarnings };
}
