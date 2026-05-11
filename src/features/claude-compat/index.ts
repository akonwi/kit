import type { PluginAPI } from "../../plugins";
import {
	discoverClaudeCommandFiles,
	readClaudeCommandPrompt,
} from "../commands/claude-commands";

export function ClaudeCompatibilityPlugin(kit: PluginAPI): void {
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
	}

	kit.addDebugSection(
		"Claude commands",
		metas.length > 0
			? metas.map((meta) => `- /cc:${meta.name} ${meta.filePath}`)
			: ["(none)"],
	);
}
