import type { AgentTool } from "@mariozechner/pi-agent-core";
import { Plugin } from "../../plugins/Plugin";
import { loadSkills, type Skill } from "./discovery";
import { formatSkillsForPrompt } from "./format";
import { createActivateSkillTool } from "./tool";

export type { Skill } from "./discovery";
export { loadSkills } from "./discovery";
export { formatSkillsForPrompt } from "./format";

export class SkillsPlugin extends Plugin {
	private skills: Skill[] = [];

	override initialize(): void {
		const cwd = this.ctx.runtime.getSession().cwd;
		const { skills, warnings } = loadSkills(cwd);
		this.skills = skills;

		for (const warning of warnings) {
			console.warn(`[skills] ${warning}`);
		}

		if (skills.length > 0) {
			// List available skills in the system prompt so the model knows
			// what's available. The prompt directs it to use the load_skill tool.
			this.addSystemPromptAddition(formatSkillsForPrompt(skills));

			// Register the load_skill tool for on-demand skill activation.
			const tool = createActivateSkillTool(() => this.skills);
			this.registerTool(tool as AgentTool);
		}
	}
}
