import { createComponent } from "solid-js";
import { DebugModal, type DebugSection } from "./DebugModal";
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

		// ── Session section ──────────────────────────────────────────

		const sessionEntries = [
			{ label: "ID", value: session.id },
			{ label: "Name", value: session.name || "(unnamed)" },
			{ label: "CWD", value: session.cwd },
			{ label: "Created", value: new Date(session.createdAt).toLocaleString() },
			{ label: "Updated", value: new Date(session.updatedAt).toLocaleString() },
		];
		if (session.parentSessionId) {
			sessionEntries.push({
				label: "Parent",
				value: session.parentSessionId,
			});
		}
		if (session.forkedFromTurnId) {
			sessionEntries.push({
				label: "Forked from",
				value: session.forkedFromTurnId,
			});
		}

		// ── Model section ────────────────────────────────────────────

		const contextValue = status.contextUsage
			? `${status.contextUsage.tokens.toLocaleString()} / ${status.contextUsage.contextWindow.toLocaleString()} tokens (${status.contextUsage.percent}%)`
			: "unknown";

		const modelSection: DebugSection = {
			title: "Model",
			entries: [
				{ label: "Model", value: runtime.getCurrentModelId() ?? "none" },
				{ label: "Thinking", value: status.thinkingLevel },
				{ label: "Context", value: contextValue },
				{ label: "Streaming", value: status.isStreaming ? "yes" : "no" },
			],
		};

		// ── Messages section ─────────────────────────────────────────

		const messagesSection: DebugSection = {
			title: "Messages",
			entries: [
				{ label: "Turns", value: String(turns.length) },
				{
					label: "Total",
					value: `${messages.length} (${userCount} user, ${assistantCount} assistant, ${toolResultCount} tool)`,
				},
				{ label: "Pending", value: String(pending) },
			],
		};

		// ── Context files section ────────────────────────────────────

		const contextFilesSection: DebugSection = {
			title: `Context Files (${contextFiles.length})`,
			entries: contextFiles.map((file) => ({
				label: "",
				value: file.path,
			})),
		};

		// ── Plugin sections ──────────────────────────────────────────

		const debugSections = runtime.getDebugSections();
		const pluginSections: DebugSection[] = [];
		for (const [name, lines] of debugSections) {
			pluginSections.push({
				title: name,
				entries: lines.map((line) => {
					const colonIndex = line.indexOf(":");
					if (colonIndex > 0) {
						return {
							label: line.slice(0, colonIndex).trim(),
							value: line.slice(colonIndex + 1).trim(),
						};
					}
					return { label: "", value: line };
				}),
			});
		}

		// ── Assemble ─────────────────────────────────────────────────

		const sections: DebugSection[] = [
			{ title: "Session", entries: sessionEntries },
			modelSection,
			messagesSection,
			...(contextFiles.length > 0 ? [contextFilesSection] : []),
			...pluginSections,
		];

		await openCustomOverlay<void>((props) =>
			createComponent(DebugModal, {
				sections,
				active: props.active,
				surfaceProps: props.surfaceProps,
				onClose: () => props.done(),
			}),
		);
	},
};
