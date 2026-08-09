import type { Command } from "./types";

export const compactCommand: Command = {
	name: "compact",
	description: "Compact session context to reduce token usage",
	// The TUI path reports failure through compaction events/toasts; RPC must
	// propagate the same failure as a rejected command response.
	async execute({ runtime }) {
		await runtime.compact();
	},
	async executeTransportNeutral({ runtime, signal }) {
		await runtime.compactOrThrow(signal);
	},
};
