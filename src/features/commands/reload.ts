// @ts-nocheck — disabled pending rewrite
/**
 * /reload command — invalidates cached indexes so newly added
 * agents, files, and commands are picked up without restarting.
 */

import type { FileIndex } from "../files";
import type { AgentIndex } from "../subagent";
import type { Command } from "./types";

export type ReloadTargets = {
	agentIndex: AgentIndex;
	fileIndex: FileIndex;
	/** Called after invalidation to refresh any derived state (e.g. claude commands). */
	onReload?: () => void;
};

export function createReloadCommand(targets: ReloadTargets): Command {
	return {
		name: "reload",
		description: "Reload agents, files, and commands",
		execute({ addNotice }) {
			targets.agentIndex.invalidate();
			targets.fileIndex.invalidate();
			targets.onReload?.();
			addNotice("info", "Reloaded", [
				"Agent index, file index, and commands refreshed.",
			]);
		},
	};
}
