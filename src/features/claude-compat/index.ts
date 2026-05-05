import { Plugin } from "../../plugins/Plugin";
import {
	discoverClaudeCommandFiles,
	discoverClaudeCommands,
} from "../commands/claude-commands";

export class ClaudeCompatibilityPlugin extends Plugin {
	override initialize(): void {
		const cwd = this.ctx.runtime.getSession().cwd;
		const metas = discoverClaudeCommandFiles(cwd);
		const commands = discoverClaudeCommands(cwd);

		for (const command of commands) {
			this.registerCommand(command);
		}

		this.setDebugSection(
			"Claude commands",
			metas.length > 0
				? metas.map((meta) => `- /cc:${meta.name} ${meta.filePath}`)
				: ["(none)"],
		);
	}
}
