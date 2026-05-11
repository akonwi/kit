import type { PluginAPI } from "../../plugins";
import { loadPromptTemplates } from "./discovery";
import { parseCommandArgs, substituteArgs } from "./substitute";

export type { PromptTemplate } from "./discovery";
export { loadPromptTemplates } from "./discovery";

export function PromptsPlugin(kit: PluginAPI): void {
	const templates = loadPromptTemplates(kit.system.cwd);

	// Register each template as a slash command
	for (const template of templates) {
		kit.registerCommand(
			template.name,
			{
				description: template.description || template.filePath,
				argName: "args",
			},
			async (ctx) => {
				const args = parseCommandArgs(ctx.args);
				const expanded = substituteArgs(template.content, args);
				await ctx.session.submitPromptCommandMessage(
					template.name,
					ctx.args,
					expanded,
				);
			},
		);
	}

	// Register debug info
	kit.addDebugSection(
		"Prompt commands",
		templates.length > 0
			? templates.map((t) => `- /${t.name} (${t.source}) ${t.filePath}`)
			: ["(none)"],
	);
}
