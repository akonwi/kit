import type { SubagentDefinition } from "./discovery";

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
		"The following sub-agents are available as named specialists.",
		"Use the subagent tool to delegate to them when isolated context would help.",
		"Use list_agents to confirm availability if needed.",
		"Use run to send the next message to a named sub-agent. It creates or continues that agent's active conversation.",
		"Use status to inspect a named sub-agent's active conversation.",
		"Use dismiss to reset a named sub-agent's active conversation.",
		"Sub-agents are context-isolated, resumable, and cannot call the subagent tool themselves.",
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
