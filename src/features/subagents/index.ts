import { Plugin } from "../../plugins/Plugin";
import { loadSubagents } from "./discovery";

export type {
	LoadSubagentsResult,
	SubagentDefinition,
	SubagentSource,
} from "./discovery";
export { loadSubagents } from "./discovery";

export class SubagentsPlugin extends Plugin {
	private clearDebugSection: (() => void) | null = null;

	override initialize(): void {
		this.subscribeRuntimeEvent("session.active.changed", async () => {
			this.refresh();
		});
		this.refresh();
	}

	override dispose(): void {
		this.clearDebugSection?.();
		this.clearDebugSection = null;
		super.dispose();
	}

	private refresh(): void {
		const cwd = this.ctx.runtime.getSession().cwd;
		const { agents, warnings } = loadSubagents(cwd);

		for (const warning of warnings) {
			console.warn(`[subagents] ${warning}`);
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
