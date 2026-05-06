import type { AgentTool } from "@mariozechner/pi-agent-core";
import { Plugin } from "../../plugins/Plugin";
import { loadSubagents, type SubagentDefinition } from "./discovery";
import { SubagentManager } from "./state";
import { createSubagentTool } from "./tool";

export type {
	LoadSubagentsResult,
	SubagentDefinition,
	SubagentSource,
} from "./discovery";
export { loadSubagents } from "./discovery";
export type {
	ActiveSubagentConversationState,
	ActiveSubagentStatus,
} from "./state";
export { SubagentManager } from "./state";
export { createSubagentTool } from "./tool";

export class SubagentsPlugin extends Plugin {
	private clearDebugSection: (() => void) | null = null;
	private unregisterTool: (() => void) | null = null;
	private readonly manager = new SubagentManager();
	private agents: SubagentDefinition[] = [];

	override initialize(): void {
		this.subscribeRuntimeEvent("session.active.changed", async () => {
			this.manager.reset();
			this.refresh();
		});
		this.refresh();
	}

	override dispose(): void {
		this.clearDebugSection?.();
		this.clearDebugSection = null;
		this.unregisterTool?.();
		this.unregisterTool = null;
		super.dispose();
	}

	private refresh(): void {
		const cwd = this.ctx.runtime.getSession().cwd;
		const { agents, warnings } = loadSubagents(cwd);
		this.agents = agents;

		for (const warning of warnings) {
			console.warn(`[subagents] ${warning}`);
		}

		this.unregisterTool?.();
		this.unregisterTool = null;
		if (agents.length > 0) {
			const tool = createSubagentTool({
				getAgents: () => this.agents,
				manager: this.manager,
			});
			this.unregisterTool = this.ctx.runtime.addTool(tool as AgentTool);
		}

		const lines = [
			...(agents.length > 0
				? agents.map(
						(agent) =>
							`- ${agent.name} (${agent.source}) ${agent.filePath}${agent.model ? ` · ${agent.model}` : ""}`,
					)
				: ["(none)"]),
			...(warnings.length > 0
				? ["Warnings:", ...warnings.map((warning) => `- ${warning}`)]
				: []),
		];

		this.clearDebugSection?.();
		this.clearDebugSection = this.ctx.runtime.setDebugSection(
			"Sub-agents",
			lines,
		);
	}
}
