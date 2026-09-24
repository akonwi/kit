/**
 * Format discovered skills for inclusion in the system prompt.
 * Uses XML format per Agent Skills standard.
 * See: https://agentskills.io/integrate-skills
 *
 * Skills with disableModelInvocation are excluded from the prompt
 * (they can only be invoked explicitly via /skill:name commands).
 */

import type { Skill } from "./discovery";

function escapeXml(str: string): string {
	return str
		.replace(/&/g, "&amp;")
		.replace(/</g, "&lt;")
		.replace(/>/g, "&gt;")
		.replace(/"/g, "&quot;")
		.replace(/'/g, "&apos;");
}

export function formatSkillsForPrompt(skills: Skill[]): string {
	const visible = skills.filter((s) => !s.disableModelInvocation);
	if (visible.length === 0) return "";

	const lines = [
		"The following skills provide specialized instructions for specific tasks.",
		"Call the activate_skill tool with the skill name to activate it when the task matches its description.",
		"When a skill's instructions reference a relative path, resolve it against the skill directory and use that absolute path in tool commands.",
		"",
		"<available_skills>",
	];

	for (const skill of visible) {
		lines.push("  <skill>");
		lines.push(`    <name>${escapeXml(skill.name)}</name>`);
		lines.push(
			`    <description>${escapeXml(skill.description)}</description>`,
		);
		lines.push(`    <location>${escapeXml(skill.filePath)}</location>`);
		lines.push("  </skill>");
	}

	lines.push("</available_skills>");
	return lines.join("\n");
}
