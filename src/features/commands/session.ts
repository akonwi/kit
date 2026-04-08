import type { Command } from "./types";

export const sessionCommand: Command = {
	name: "session",
	description: "Show current session details",
	execute({ runtime, palette }) {
		const session = runtime.getSession();
		const turns = runtime.getTurns();
		const messages = runtime.getMessages();
		const status = runtime.getStatus();
		const pending = runtime.getPendingMessageCount();

		const userCount = messages.filter(
			(m) => "role" in m && m.role === "user",
		).length;
		const assistantCount = messages.filter(
			(m) => "role" in m && m.role === "assistant",
		).length;
		const toolResultCount = messages.filter(
			(m) => "role" in m && m.role === "toolResult",
		).length;

		const contextLine = status.contextUsage
			? `Context: ${status.contextUsage.tokens.toLocaleString()} / ${status.contextUsage.contextWindow.toLocaleString()} tokens (${status.contextUsage.percent}%)`
			: "Context: unknown";

		palette.show({
			mode: "modal",
			title: "Session",
			lines: [
				`ID: ${session.id}`,
				`Name: ${session.name || "(unnamed)"}`,
				`CWD: ${session.cwd}`,
				`Model: ${runtime.getCurrentModelId() ?? "none"}`,
				`Thinking: ${status.thinkingLevel}`,
				contextLine,
				`Streaming: ${status.isStreaming ? "yes" : "no"}`,
				`Pending queued messages: ${pending}`,
				`Turns: ${turns.length}`,
				`Messages: ${messages.length} total (${userCount} user, ${assistantCount} assistant, ${toolResultCount} tool results)`,
				`Created: ${new Date(session.createdAt).toLocaleString()}`,
				`Updated: ${new Date(session.updatedAt).toLocaleString()}`,
			],
		});
	},
};
