import type { InternalPluginAPI } from "../../plugins";
import type { PromptTemplate } from "./discovery";
import { loadPromptTemplates } from "./discovery";
import { parseCommandArgs, substituteArgs } from "./substitute";

export type { PromptTemplate } from "./discovery";
export { loadPromptTemplates } from "./discovery";

function expandPromptTemplate(template: PromptTemplate, args: string): string {
	return substituteArgs(template.content, parseCommandArgs(args));
}

export function PromptsPlugin(kit: InternalPluginAPI): () => void {
	const contributionDisposers: Array<() => void> = [];

	function clearContributions(): void {
		for (const dispose of contributionDisposers.splice(0)) dispose();
	}

	function registerWorkspaceTemplates(cwd: string): void {
		const templates = loadPromptTemplates(cwd);
		clearContributions();

		for (const template of templates) {
			contributionDisposers.push(
				kit.registerCommand(
					template.name,
					{
						description: template.description || template.filePath,
						argName: "args",
						executeTransportNeutral: ({
							args,
							schedulePromptCommand,
							signal,
						}) => {
							signal?.throwIfAborted();
							schedulePromptCommand(
								template.name,
								args,
								expandPromptTemplate(template, args),
							);
							signal?.throwIfAborted();
						},
					},
					async (ctx) => {
						await ctx.session.submitPromptCommandMessage(
							template.name,
							ctx.args,
							expandPromptTemplate(template, ctx.args),
						);
					},
				),
			);
		}

		contributionDisposers.push(
			kit.addDebugSection(
				"Prompt commands",
				templates.length > 0
					? templates.map(
							(template) =>
								`- /${template.name} (${template.source}) ${template.filePath}`,
						)
					: ["(none)"],
			),
		);
	}

	registerWorkspaceTemplates(kit.system.cwd);
	kit.on("session.active.changed.cwd", (event) => {
		registerWorkspaceTemplates(event.cwd);
	});
	return clearContributions;
}
