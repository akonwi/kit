import type { LoadMcpConfigResult, McpServerRuntimeState } from "./types";

export type McpPanelData = {
	getStates: () => McpServerRuntimeState[];
	getConfig: () => LoadMcpConfigResult | null;
	hasOAuthSession: (serverName: string) => boolean;
	subscribeToChanges: (listener: () => void) => () => void;
};

export type McpWorkspaceController = {
	/** Returns whether a mounted workspace handled the request. */
	open(): boolean;
	onOpenRequest(listener: () => void): () => void;
	setData(data: McpPanelData | null): void;
	data(): McpPanelData | null;
	subscribe(listener: () => void): () => void;
};

/** Bridges the MCP plugin's lifecycle-owned state into the retained workspace. */
export function createMcpWorkspaceController(): McpWorkspaceController {
	let data: McpPanelData | null = null;
	const openListeners = new Set<() => void>();
	const dataListeners = new Set<() => void>();

	return {
		open() {
			if (openListeners.size === 0) return false;
			for (const listener of [...openListeners]) listener();
			return true;
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
