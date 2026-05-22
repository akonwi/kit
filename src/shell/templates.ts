import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { getKitPaths } from "../paths";

/**
 * Pre-loaded template cache.
 *
 * Templates are resolved at startup: .kit/templates/<name>.md (project)
 * wins over ~/.kit/templates/<name>.md (user), which wins over the
 * built-in default.
 *
 * The only variable supported today is {{content}}.
 */

type TemplateCache = Map<string, string>;

let cache: TemplateCache | null = null;

/** Built-in defaults that ship with Kit. */
export const BUILT_IN_TEMPLATES: Record<string, string> = {
	"review-feedback": ["Here is my feedback to code.", "", "{{content}}"].join(
		"\n",
	),
	"pager-feedback": [
		"Here is my feedback on your previous response, grouped by section.",
		"",
		"{{content}}",
	].join("\n"),
};

function resolveTemplateFile(
	name: string,
	projectDir: string | undefined,
): string | null {
	const paths = getKitPaths();

	// 1. Project template
	if (projectDir) {
		const projectPath = path.join(
			projectDir,
			".kit",
			"templates",
			`${name}.md`,
		);
		if (existsSync(projectPath)) return projectPath;
	}

	// 2. User template
	const userPath = path.join(paths.kitRoot, "templates", `${name}.md`);
	if (existsSync(userPath)) return userPath;

	return null;
}

function loadTemplateFile(filePath: string): string {
	return readFileSync(filePath, "utf8");
}

/**
 * Initialize the template cache.
 * Call once at bootstrap with the current project directory.
 * Safe to call multiple times (overwrites cache).
 */
export function initTemplates(projectDir: string | undefined): void {
	cache = new Map();
	for (const name of Object.keys(BUILT_IN_TEMPLATES)) {
		const filePath = resolveTemplateFile(name, projectDir);
		cache.set(
			name,
			filePath ? loadTemplateFile(filePath) : BUILT_IN_TEMPLATES[name],
		);
	}
}

function applyVariables(
	template: string,
	variables: Record<string, string>,
): string {
	let result = template;
	for (const [key, value] of Object.entries(variables)) {
		result = result.replaceAll(`{{${key}}}`, value);
	}
	return result;
}

/**
 * Render a named template with variable substitution.
 * Templates must be initialized first via initTemplates().
 */
export function renderTemplate(
	name: string,
	variables: Record<string, string>,
): string | null {
	const template = cache?.get(name) ?? BUILT_IN_TEMPLATES[name];
	if (!template) return null;
	return applyVariables(template, variables);
}
