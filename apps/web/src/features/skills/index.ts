import type { InternalPluginAPI } from "../../plugins";
import { loadSkills } from "./discovery";
import { formatSkillsForPrompt } from "./format";
import { createActivateSkillTool } from "./tool";

export type { Skill } from "./discovery";
export { loadSkills } from "./discovery";
export { formatSkillsForPrompt } from "./format";

export function SkillsPlugin(kit: InternalPluginAPI): void {
	const { skills, warnings } = loadSkills(kit.system.cwd);

	for (const warning of warnings) {
		console.warn(`[skills] ${warning}`);
	}

	if (skills.length > 0) {
		// List available skills in the system prompt so the model knows
		// what's available. The prompt directs it to use activate_skill.
		kit.addSystemPrompt(formatSkillsForPrompt(skills));

		// Register the activate_skill tool for on-demand skill activation.
		const tool = createActivateSkillTool(() => skills);
		kit.registerTool(tool);
	}

	// Register debug info for /debug command
	kit.addDebugSection(
		"Skills",
		skills.length > 0
			? skills.map((s) => `- ${s.name} (${s.source}) ${s.filePath}`)
			: ["(none)"],
	);
}
