import type { InternalPluginAPI } from "../../plugins";
import {
	discoverClaudeCommandFiles,
	expandRemoteClaudeCommandPrompt,
	readClaudeCommandPrompt,
} from "../commands/claude-commands";
import { loadSkills } from "../skills/discovery";

export function ClaudeCompatibilityPlugin(kit: InternalPluginAPI): () => void {
	const contributionDisposers: Array<() => void> = [];

	function clearContributions(): void {
		for (const dispose of contributionDisposers.splice(0)) dispose();
	}

	function registerWorkspaceCommands(cwd: string): void {
		const metas = discoverClaudeCommandFiles(cwd);
		clearContributions();

		for (const meta of metas) {
			const commandName = `cc:${meta.name}`;
			contributionDisposers.push(
				kit.registerCommand(
					commandName,
					{
						description: meta.description,
						...(meta.argName ? { argName: meta.argName } : {}),
						executeTransportNeutral: ({
							args,
							schedulePromptCommand,
							signal,
						}) => {
							const prompt = expandRemoteClaudeCommandPrompt(
								meta.rawContent,
								args,
								signal,
							);
							if (!prompt) {
								throw new Error(
									`Claude-compatible command is empty: /${commandName}`,
								);
							}
							schedulePromptCommand(commandName, args, prompt);
							signal?.throwIfAborted();
						},
					},
					async (ctx) => {
						const prompt = readClaudeCommandPrompt(meta.filePath, ctx.args);
						if (prompt) {
							await ctx.session.submitPromptCommandMessage(
								commandName,
								ctx.args,
								prompt,
							);
						}
					},
				),
			);
		}

		const claudeSkills = loadSkills(cwd).skills.filter(
			(skill) => skill.source === "claude-compat",
		);
		contributionDisposers.push(
			kit.addDebugSection(
				"Claude skills",
				claudeSkills.length > 0
					? claudeSkills.map((skill) => `- ${skill.name} ${skill.filePath}`)
					: ["(none)"],
			),
			kit.addDebugSection(
				"Claude commands",
				metas.length > 0
					? metas.map((meta) => `- /cc:${meta.name} ${meta.filePath}`)
					: ["(none)"],
			),
		);
	}

	registerWorkspaceCommands(kit.system.cwd);
	kit.on("session.active.changed.cwd", (event) => {
		registerWorkspaceCommands(event.cwd);
	});
	return clearContributions;
}
