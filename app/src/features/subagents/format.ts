import type { SubagentDefinition } from "./discovery";

const SUBAGENT_PROMPT_INTRO =
	"The following sub-agents are available as named specialists.";

export function isSubagentPromptAddition(text: string): boolean {
	return (
		text.startsWith(`${SUBAGENT_PROMPT_INTRO}\n`) &&
		text.includes("<available_subagents>")
	);
}

function escapeXml(str: string): string {
	return str
		.replace(/&/g, "&amp;")
		.replace(/</g, "&lt;")
		.replace(/>/g, "&gt;")
		.replace(/"/g, "&quot;")
		.replace(/'/g, "&apos;");
}

export function formatSubagentsForPrompt(agents: SubagentDefinition[]): string {
	if (agents.length === 0) return "";

	const lines = [
		SUBAGENT_PROMPT_INTRO,
		"Use the subagent tools to manage and use them.",
		"",
		"<available_subagents>",
	];

	for (const agent of agents) {
		lines.push("  <subagent>");
		lines.push(`    <name>${escapeXml(agent.name)}</name>`);
		lines.push(
			`    <description>${escapeXml(agent.description)}</description>`,
		);
		if (agent.model) {
			lines.push(`    <model>${escapeXml(agent.model)}</model>`);
		}
		lines.push(`    <source>${escapeXml(agent.source)}</source>`);
		lines.push("  </subagent>");
	}

	lines.push("</available_subagents>");
	return lines.join("\n");
}
