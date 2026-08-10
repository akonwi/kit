import type { Command } from "./types";

export const reloadCommand: Command = {
	name: "reload",
	description: "Reload the current session and refresh plugin state",
	async execute({ _reload }) {
		await _reload();
	},
	async executeTransportNeutral({ reloadHost, signal }) {
		if (!reloadHost) throw new Error("Host reload is unavailable");
		await reloadHost(signal);
	},
	transportNeutralTimeoutMs: null,
	transportNeutralCancellation: "settle",
};
