import type { AgentToolResult } from "@mariozechner/pi-agent-core";
import { type Static, Type } from "@sinclair/typebox";
import type { SubagentDefinition } from "./discovery";
import type { ActiveSubagentStatus, SubagentManager } from "./state";

const parameters = Type.Object({
	action: Type.Union([
		Type.Literal("list_agents"),
		Type.Literal("status"),
		Type.Literal("dismiss"),
	]),
	agent: Type.Optional(
		Type.String({ description: "Name of the sub-agent to inspect or dismiss" }),
	),
});

type Parameters = Static<typeof parameters>;

type ListedSubagent = {
	name: string;
	description: string;
	model?: string;
	source: SubagentDefinition["source"];
};

function textResult(
	text: string,
	details: Record<string, unknown>,
): AgentToolResult<Record<string, unknown>> {
	return {
		content: [{ type: "text" as const, text }],
		details,
	};
}

function renderStatus(status: ActiveSubagentStatus): string {
	switch (status) {
		case "idle":
			return "idle";
		case "running":
			return "running";
		case "failed":
			return "failed";
		case "aborted":
			return "aborted";
	}
}

function requireAgent(input: Parameters): string | null {
	if (typeof input.agent !== "string") return null;
	const agent = input.agent.trim();
	return agent.length > 0 ? agent : null;
}

export function createSubagentTool(options: {
	getAgents: () => SubagentDefinition[];
	manager: SubagentManager;
}) {
	return {
		name: "subagent",
		label: "Sub-agent",
		description:
			"List available sub-agents and inspect or dismiss active delegated sub-agent conversations.",
		promptSnippet:
			"Inspect available sub-agents or the current active state of a named sub-agent.",
		promptGuidelines: [
			"Use list_agents to discover available named sub-agents before relying on one.",
			"Use status to inspect whether a named sub-agent currently has an active delegated conversation.",
			"Use dismiss to reset an active delegated conversation when you explicitly want to discard its active state.",
		],
		parameters,
		async execute(
			_toolCallId: string,
			input: Parameters,
		): Promise<AgentToolResult<Record<string, unknown>>> {
			if (input.action === "list_agents") {
				const agents = options.getAgents().map<ListedSubagent>((agent) => ({
					name: agent.name,
					description: agent.description,
					model: agent.model,
					source: agent.source,
				}));
				if (agents.length === 0) {
					return textResult("No sub-agents are currently available.", {
						ok: true,
						action: "list_agents",
						agents,
					});
				}
				return textResult(
					[
						`Available sub-agents (${agents.length}):`,
						"",
						...agents.map((agent) =>
							agent.model
								? `- ${agent.name} — ${agent.description} (${agent.source}, ${agent.model})`
								: `- ${agent.name} — ${agent.description} (${agent.source})`,
						),
					].join("\n"),
					{
						ok: true,
						action: "list_agents",
						agents,
					},
				);
			}

			const agent = requireAgent(input);
			if (!agent) {
				return textResult("Provide a sub-agent name.", {
					ok: false,
					action: input.action,
					code: "INVALID_INPUT",
					message: "Provide a sub-agent name.",
				});
			}

			if (input.action === "status") {
				const active = options.manager.getActive(agent);
				if (!active) {
					return textResult(
						`No active delegated conversation for sub-agent "${agent}".`,
						{
							ok: true,
							action: "status",
							agent,
							active: false,
						},
					);
				}
				return textResult(
					[
						`Sub-agent "${agent}" is active.`,
						`Status: ${renderStatus(active.status)}`,
						...(active.model ? [`Model: ${active.model}`] : []),
						`Last activity: ${active.lastActivityAt}`,
					].join("\n"),
					{
						ok: true,
						action: "status",
						agent,
						active: true,
						status: active.status,
						model: active.model,
						lastActivityAt: active.lastActivityAt,
					},
				);
			}

			const dismissed = options.manager.dismiss(agent);
			return textResult(
				dismissed
					? `Dismissed active sub-agent conversation for "${agent}".`
					: `No active delegated conversation for sub-agent "${agent}".`,
				{
					ok: true,
					action: "dismiss",
					agent,
					dismissed,
				},
			);
		},
	};
}
