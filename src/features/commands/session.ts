import { createComponent } from "solid-js";
import { DebugModal } from "./DebugModal";
import type { Command } from "./types";

export const sessionCommand: Command = {
	name: "debug",
	description: "Show runtime and session debug details",
	async execute({ runtime, openCustomOverlay }) {
		const session = runtime.getSession();
		const turns = runtime.getTurns();
		const messages = runtime.getMessages();
		const status = runtime.getStatus();
		const pending = runtime.getPendingMessageCount();
		const contextFiles = runtime.getContextFiles();

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

		const debugSections = runtime.getDebugSections();
		const pluginLines: string[] = [];
		for (const [section, lines] of debugSections) {
			pluginLines.push(`${section}:`);
			pluginLines.push(...lines);
		}

		const lines = [
			`ID: ${session.id}`,
			`Name: ${session.name || "(unnamed)"}`,
			`Parent: ${session.parentSessionId ?? "(none)"}`,
			...(session.forkedFromTurnId
				? [`Forked from turn: ${session.forkedFromTurnId}`]
				: []),
			`CWD: ${session.cwd}`,
			`Model: ${runtime.getCurrentModelId() ?? "none"}`,
			`Thinking: ${status.thinkingLevel}`,
			contextLine,
			`Streaming: ${status.isStreaming ? "yes" : "no"}`,
			`Pending queued messages: ${pending}`,
			`Turns: ${turns.length}`,
			`Messages: ${messages.length} total (${userCount} user, ${assistantCount} assistant, ${toolResultCount} tool results)`,
			`Context files: ${contextFiles.length}`,
			...contextFiles.map((file) => `- ${file.path}`),
			...pluginLines,
			`Created: ${new Date(session.createdAt).toLocaleString()}`,
			`Updated: ${new Date(session.updatedAt).toLocaleString()}`,
		];

		await openCustomOverlay<void>((props) =>
			createComponent(DebugModal, {
				title: "Debug",
				lines,
				onClose: () => props.done(),
			}),
		);
	},
};
