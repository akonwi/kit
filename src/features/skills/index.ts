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
			// what's available. The prompt directs it to use activate_skill.
			this.addSystemPromptAddition(formatSkillsForPrompt(skills));

			// Register the activate_skill tool for on-demand skill activation.
			const tool = createActivateSkillTool(() => this.skills);
			this.registerTool(tool as AgentTool);
		}

		// Register debug info for /debug command
		this.setDebugSection(
			"Skills",
			skills.length > 0
				? skills.map((s) => `- ${s.name} (${s.source}) ${s.filePath}`)
				: ["(none)"],
		);
	}
}
