/**
 * Bridges the subagents plugin and the app shell's workspace panel.
 *
 * The plugin registers its data providers here during initialization and
 * requests the panel to open from the `/subagents` command; the shell
 * subscribes to both so it can mount the panel and react to plugin
 * reloads without a direct dependency on plugin lifecycle.
 */

import type { SessionEntry } from "../../session";
import type { SubagentDefinition } from "./discovery";
import type { ActiveSubagentConversationState } from "./state";

export type SubagentsPanelData = {
	getAgents: () => SubagentDefinition[];
	getActiveConversations: () => ActiveSubagentConversationState[];
	readConversationEntries: (conversationId: string) => Promise<SessionEntry[]>;
	subscribeToChanges: (listener: () => void) => () => void;
	dismissConversation: (agentName: string) => Promise<boolean>;
};

export type SubagentsWorkspaceController = {
	/** Ask the shell to open (or focus) the sub-agents workspace pane. */
	open(): void;
	onOpenRequest(listener: () => void): () => void;
	/** Register or clear the plugin's data providers. */
	setData(data: SubagentsPanelData | null): void;
	data(): SubagentsPanelData | null;
	/** Notifies when the registered data providers change (plugin reloads). */
	subscribe(listener: () => void): () => void;
};

export function createSubagentsWorkspaceController(): SubagentsWorkspaceController {
	let data: SubagentsPanelData | null = null;
	const openListeners = new Set<() => void>();
	const dataListeners = new Set<() => void>();
	return {
		open() {
			for (const listener of [...openListeners]) listener();
		},
		onOpenRequest(listener) {
			openListeners.add(listener);
			return () => openListeners.delete(listener);
		},
		setData(next) {
			data = next;
			for (const listener of [...dataListeners]) listener();
		},
		data: () => data,
		subscribe(listener) {
			dataListeners.add(listener);
			return () => dataListeners.delete(listener);
		},
	};
}
