import { Plugin } from "../../plugins/Plugin";
import type { CommandContext } from "../commands/types";
import { loadPromptTemplates, type PromptTemplate } from "./discovery";
import { parseCommandArgs, substituteArgs } from "./substitute";

export type { PromptTemplate } from "./discovery";
export { loadPromptTemplates } from "./discovery";

export class PromptsPlugin extends Plugin {
	private templates: PromptTemplate[] = [];

	override initialize(): void {
		const cwd = this.ctx.runtime.getSession().cwd;
		this.templates = loadPromptTemplates(cwd);

		// Register each template as a slash command
		for (const template of this.templates) {
			this.registerCommand({
				name: template.name,
				description: template.description || template.filePath,
				argName: "args",
				execute: async (ctx: CommandContext) => {
					const args = parseCommandArgs(ctx.args);
					const expanded = substituteArgs(template.content, args);
					await this.ctx.runtime.submitPromptCommandMessage(
						template.name,
						ctx.args,
						expanded,
					);
				},
			});
		}

		// Register debug info
		this.setDebugSection(
			"Prompt commands",
			this.templates.length > 0
				? this.templates.map((t) => `- /${t.name} (${t.source}) ${t.filePath}`)
				: ["(none)"],
		);
	}
}
