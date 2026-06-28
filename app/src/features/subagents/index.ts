import { createComponent } from "solid-js/web";
import type {
	InternalPluginAPI,
	InternalPluginDefinition,
} from "../../plugins";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import { loadSubagents, type SubagentDefinition } from "./discovery";
import { formatSubagentsForPrompt } from "./format";
import { SubagentsStatusModal } from "./SubagentsStatusModal";
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
}): InternalPluginDefinition {
	return function SubagentsPlugin(kit: InternalPluginAPI): () => void {
		let clearDebugSection: (() => void) | null = null;
		let unregisterTool: (() => void) | null = null;
		let removePromptAddition: (() => void) | null = null;
		let agents: SubagentDefinition[] = [];
		let disposed = false;

		const manager = new SubagentManager({
			runtime: options.runtime,
			getAgents: () => agents,
		});

		// Synchronously seed file agents before subsequent plugins initialize
		// so registerSubagent can check against the complete set immediately.
		const { agents: initialFileAgents } = loadSubagents(kit.system.cwd);
		options.runtime.setDiscoveredSubagents(initialFileAgents);

		let warnings: string[] = [];

		function updateContributions(): void {
			if (disposed) return;
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
				unregisterTool = kit.registerTool(tool);
			}

			const lines = [
				...(agents.length > 0
					? agents.map((agent) => {
							const src =
								agent.source === "plugin"
									? `(plugin:${agent.pluginName ?? "?"})`
									: `(${agent.source}) ${agent.filePath ?? ""}`;
							return `- ${agent.name} ${src}${agent.model ? ` · ${agent.model}` : ""}`;
						})
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

		async function refresh(refreshOptions?: {
			hydrate?: boolean;
		}): Promise<void> {
			const shouldHydrate = refreshOptions?.hydrate ?? true;
			const { agents: fileAgents, warnings: nextWarnings } = loadSubagents(
				kit.system.cwd,
			);
			warnings = nextWarnings;
			options.runtime.setDiscoveredSubagents(fileAgents);

			const pluginAgents = options.runtime.getPluginSubagents();
			const seenNames = new Set<string>();
			const merged: SubagentDefinition[] = [];

			// File-discovered agents first — they win on name conflict
			for (const agent of fileAgents) {
				seenNames.add(agent.name);
				merged.push(agent);
			}

			// Plugin-contributed agents — skip any that conflict with file agents
			for (const agent of pluginAgents) {
				if (seenNames.has(agent.name)) continue;
				seenNames.add(agent.name);
				merged.push(agent);
			}

			agents = merged;
			if (shouldHydrate) {
				await manager.hydrate(kit.session.get());
			}
			if (disposed) return;

			for (const warning of warnings) {
				console.warn(`[subagents] ${warning}`);
			}

			updateContributions();
		}

		kit.on("session.active.changed", async () => {
			await refresh();
		});
		kit.on("subagents.changed", async () => {
			await refresh({ hydrate: false });
		});

		kit.registerCommand(
			"subagents",
			{
				description: "Show sub-agents and active sub-agent conversations",
			},
			async (ctx) => {
				if (ctx.args.trim()) {
					ctx.ui.toast({
						title: "Sub-agents",
						subtitle: "Use /subagents with no arguments.",
						variant: "warning",
					});
					return;
				}

				await ctx.ui.custom<void>((props) =>
					createComponent(SubagentsStatusModal, {
						surfaceProps: props.surfaceProps,
						getAgents: () => agents,
						getActiveConversations: () => manager.listActive(),
						get active() {
							return props.active;
						},
						onClose: () => props.done(undefined),
					}),
				);
			},
		);

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
