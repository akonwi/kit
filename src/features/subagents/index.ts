import type { AgentTool } from "@mariozechner/pi-agent-core";
import type { PluginAPI, PluginDefinition } from "../../plugins";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { loadSubagents, type SubagentDefinition } from "./discovery";
import { formatSubagentsForPrompt } from "./format";
import { SubagentManager } from "./state";
import { createSubagentTool } from "./tool";

export type {
	LoadSubagentsResult,
	SubagentDefinition,
	SubagentSource,
} from "./discovery";
export { loadSubagents } from "./discovery";
export { formatSubagentsForPrompt } from "./format";
export type {
	ActiveSubagentConversationState,
	ActiveSubagentStatus,
	SubagentRunResult,
} from "./state";
export { SubagentManager, SubagentManagerError } from "./state";
export { createSubagentTool } from "./tool";

export function createSubagentsPlugin(options: {
	runtime: AgentRuntime;
}): PluginDefinition {
	return function SubagentsPlugin(kit: PluginAPI): () => void {
		let clearDebugSection: (() => void) | null = null;
		let unregisterTool: (() => void) | null = null;
		let removePromptAddition: (() => void) | null = null;
		let agents: SubagentDefinition[] = [];
		let disposed = false;

		const manager = new SubagentManager({
			runtime: options.runtime,
			getAgents: () => agents,
		});

		async function refresh(): Promise<void> {
			const { agents: nextAgents, warnings } = loadSubagents(kit.system.cwd);
			agents = nextAgents;
			await manager.hydrate(kit.session.get());
			if (disposed) return;

			for (const warning of warnings) {
				console.warn(`[subagents] ${warning}`);
			}

			unregisterTool?.();
			unregisterTool = null;
			removePromptAddition?.();
			removePromptAddition = null;
			const active = manager.listActive();
			const promptAddition = formatSubagentsForPrompt(agents);
			if (promptAddition) {
				removePromptAddition = kit.addSystemPrompt(promptAddition);
			}
			if (agents.length > 0 || active.length > 0) {
				const tool = createSubagentTool({
					getAgents: () => agents,
					manager,
				});
				unregisterTool = kit.registerTool(tool as AgentTool);
			}

			const lines = [
				...(agents.length > 0
					? agents.map(
							(agent) =>
								`- ${agent.name} (${agent.source}) ${agent.filePath}${agent.model ? ` · ${agent.model}` : ""}`,
						)
					: ["(none)"]),
				...(active.length > 0
					? [
							"",
							"Active conversations:",
							...active.map(
								(conversation) =>
									`- ${conversation.agentName} · ${conversation.status} · ${conversation.lastActivityAt}`,
							),
						]
					: []),
				...(warnings.length > 0
					? ["", "Warnings:", ...warnings.map((warning) => `- ${warning}`)]
					: []),
			];

			clearDebugSection?.();
			clearDebugSection = kit.addDebugSection("Sub-agents", lines);
		}

		kit.on("session.active.changed", async () => {
			await refresh();
		});
		void refresh();

		return () => {
			disposed = true;
			manager.reset();
			clearDebugSection?.();
			clearDebugSection = null;
			unregisterTool?.();
			unregisterTool = null;
			removePromptAddition?.();
			removePromptAddition = null;
		};
	};
}
