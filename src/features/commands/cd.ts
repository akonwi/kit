import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { homedir } from "node:os";
import type { Command } from "./types";

export const cdCommand: Command = {
	name: "cd",
	description: "Change working directory",
	execute({ runtime, palette }) {
		const currentCwd = process.cwd();

		palette.show({
			mode: "input",
			label: "Directory",
			inputValue: currentCwd,
			onSubmit: async (value, ctx) => {
				ctx.dismiss();

				const raw = value.trim();
				if (!raw) return;

				// Expand ~ to home directory
				const expanded = raw.startsWith("~")
					? resolve(homedir(), raw.slice(1).replace(/^\//, ""))
					: resolve(currentCwd, raw);

				if (!existsSync(expanded)) {
					console.error(`cd: directory not found: ${expanded}`);
					return;
				}

				try {
					const agentSession = runtime.getAgentSession();

					// 1. Mutate the private cwd on agent session and session manager
					(agentSession as any)._cwd = expanded;
					(agentSession.sessionManager as any).cwd = expanded;

					// 2. Change the actual process cwd
					process.chdir(expanded);

					// 3. Reload to recreate tools with new cwd
					await agentSession.reload();

					console.log(`cd: changed to ${expanded}`);
				} catch (err) {
					console.error(`cd failed: ${err}`);
				}
			},
		});
	},
};
