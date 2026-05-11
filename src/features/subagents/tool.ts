import { type Static, Type } from "@mariozechner/pi-ai";
import type { PluginToolDefinition, PluginToolResult } from "../../plugins";
import type { SubagentDefinition } from "./discovery";
import {
	type ActiveSubagentStatus,
	type SubagentManager,
	SubagentManagerError,
} from "./state";

const parameters = Type.Object({
	action: Type.Union([
		Type.Literal("list_agents"),
		Type.Literal("run"),
		Type.Literal("status"),
		Type.Literal("dismiss"),
	]),
	agent: Type.Optional(
		Type.String({
			description: "Name of the sub-agent to run, inspect, or dismiss",
		}),
	),
	message: Type.Optional(
		Type.String({ description: "Message to send to the named sub-agent" }),
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
): PluginToolResult<Record<string, unknown>> {
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

function requireMessage(input: Parameters): string | null {
	if (typeof input.message !== "string") return null;
	const message = input.message.trim();
	return message.length > 0 ? message : null;
}

function toolError(
	action: Parameters["action"],
	error: unknown,
): PluginToolResult<Record<string, unknown>> {
	if (error instanceof SubagentManagerError) {
		return textResult(error.message, {
			ok: false,
			action,
			code: error.code,
			message: error.message,
		});
	}
	const message = error instanceof Error ? error.message : String(error);
	return textResult(message, {
		ok: false,
		action,
		code: "RUNTIME_ERROR",
		message,
	});
}

export function createSubagentTool(options: {
	getAgents: () => SubagentDefinition[];
	manager: Pick<SubagentManager, "dismiss" | "getActive" | "run">;
}): PluginToolDefinition<typeof parameters, Record<string, unknown>> {
	return {
		name: "subagent",
		label: "Sub-agent",
		description:
			"List available sub-agents and run, inspect, or dismiss active delegated sub-agent conversations.",
		promptSnippet:
			"Delegate work to a named sub-agent, inspect its active state, or reset it.",
		promptGuidelines: [
			"Use list_agents to discover available named sub-agents before relying on one.",
			"Use run to send a message to a named sub-agent. It creates a new active conversation when needed and otherwise continues the active one.",
			"Use status to inspect whether a named sub-agent currently has an active delegated conversation.",
			"Use dismiss to reset an active delegated conversation when you explicitly want to discard its active state.",
		],
		parameters,
		async execute(
			_toolCallId: string,
			input: Parameters,
		): Promise<PluginToolResult<Record<string, unknown>>> {
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

			if (input.action === "run") {
				const message = requireMessage(input);
				if (!message) {
					return textResult("Provide a sub-agent message.", {
						ok: false,
						action: "run",
						code: "INVALID_INPUT",
						message: "Provide a sub-agent message.",
					});
				}
				try {
					const result = await options.manager.run(agent, message);
					return textResult(
						result.status === "completed"
							? result.message?.trim() ||
									`Sub-agent "${agent}" completed without a text response.`
							: result.error ||
									result.message ||
									`Sub-agent "${agent}" ${result.status}.`,
						{
							ok: true,
							action: "run",
							agent,
							status: result.status,
							...(result.message ? { message: result.message } : {}),
							...(result.error ? { error: result.error } : {}),
						},
					);
				} catch (error) {
					return toolError("run", error);
				}
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
						...(active.latestMessage
							? ["", `Latest message:\n${active.latestMessage}`]
							: []),
					].join("\n"),
					{
						ok: true,
						action: "status",
						agent,
						active: true,
						status: active.status,
						model: active.model,
						lastActivityAt: active.lastActivityAt,
						...(active.latestMessage
							? { latestMessage: active.latestMessage }
							: {}),
					},
				);
			}

			try {
				const dismissed = await options.manager.dismiss(agent);
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
			} catch (error) {
				return toolError("dismiss", error);
			}
		},
	};
}
