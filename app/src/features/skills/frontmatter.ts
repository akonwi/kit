/**
 * YAML frontmatter parser for SKILL.md files.
 */

import { parse } from "yaml";

export type Frontmatter = Record<string, unknown>;

/**
 * Extract YAML frontmatter and body from a markdown file.
 * Returns empty frontmatter if no `---` delimiters are found.
 */
export function parseFrontmatter(content: string): {
	frontmatter: Frontmatter;
	body: string;
} {
	const normalized = content.replace(/\r\n/g, "\n").replace(/\r/g, "\n");

	if (!normalized.startsWith("---")) {
		return { frontmatter: {}, body: normalized };
	}

	const endIndex = normalized.indexOf("\n---", 3);
	if (endIndex === -1) {
		return { frontmatter: {}, body: normalized };
	}

	const yamlString = normalized.slice(4, endIndex);
	const body = normalized.slice(endIndex + 4).trim();

	try {
		const parsed = parse(yamlString);
		const frontmatter =
			parsed && typeof parsed === "object" ? (parsed as Frontmatter) : {};
		return { frontmatter, body };
	} catch {
		return { frontmatter: {}, body };
	}
}
