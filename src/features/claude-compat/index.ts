import type { InternalPluginAPI } from "../../plugins";
import {
	discoverClaudeCommandFiles,
	readClaudeCommandPrompt,
} from "../commands/claude-commands";
import { loadSkills } from "../skills/discovery";

export function ClaudeCompatibilityPlugin(kit: InternalPluginAPI): void {
	const cwd = kit.system.cwd;
	const metas = discoverClaudeCommandFiles(cwd);

	for (const meta of metas) {
		kit.registerCommand(
			`cc:${meta.name}`,
			{
				description: meta.description,
				...(meta.argName ? { argName: meta.argName } : {}),
			},
			async (ctx) => {
				const prompt = readClaudeCommandPrompt(meta.filePath, ctx.args);
				if (prompt) {
					await ctx.session.submitPromptCommandMessage(
						`cc:${meta.name}`,
						ctx.args,
						prompt,
					);
				}
			},
		);

		// Skill discovery is centralized in SkillsPlugin; here we just surface
		// which skills came from Claude-compat locations for debugging.
		const claudeSkills = loadSkills(cwd).skills.filter(
			(s) => s.source === "claude-compat",
		);
		kit.addDebugSection(
			"Claude skills",
			claudeSkills.length > 0
				? claudeSkills.map((s) => `- ${s.name} ${s.filePath}`)
				: ["(none)"],
		);
	}

	kit.addDebugSection(
		"Claude commands",
		metas.length > 0
			? metas.map((meta) => `- /cc:${meta.name} ${meta.filePath}`)
			: ["(none)"],
	);
}
