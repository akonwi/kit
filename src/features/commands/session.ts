// @ts-nocheck — disabled pending rewrite
import type { Command } from "./types";

export const sessionCommand: Command = {
	name: "session",
	description: "Show current session details",
	execute({ runtime, addNotice }) {
		const agentSession = runtime.getAgentSession();
		const session = runtime.getSession();
		const usage = agentSession.getContextUsage();
		const model = agentSession.model;
		const messages = runtime.getMessages();

		const userCount = messages.filter(
			(m) => "role" in m && m.role === "user",
		).length;
		const assistantCount = messages.filter(
			(m) => "role" in m && m.role === "assistant",
		).length;
		const toolResultCount = messages.filter(
			(m) => "role" in m && m.role === "toolResult",
		).length;

		const lines: string[] = [
			`ID:       ${session.sessionId}`,
			`Name:     ${session.sessionName || "(unnamed)"}`,
			`File:     ${agentSession.sessionFile || "(none)"}`,
			`CWD:      ${agentSession.sessionManager.getCwd()}`,
			`Model:    ${model?.name ?? model?.id ?? "none"} (${model?.provider ?? "?"})`,
			`Thinking: ${agentSession.thinkingLevel ?? "off"}`,
			`Context:  ${usage?.tokens != null ? `${usage.tokens} tokens` : "unknown"} / ${usage?.contextWindow ?? "?"} (${usage?.percent != null ? `${Math.round(usage.percent)}%` : "–"})`,
			`Messages: ${messages.length} total (${userCount} user, ${assistantCount} assistant, ${toolResultCount} tool results)`,
		];

		addNotice("info", "Session", lines);
	},
};
