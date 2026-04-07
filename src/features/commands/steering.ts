// @ts-nocheck — disabled pending rewrite
/**
 * /steer and /followup commands — queue messages for the agent.
 */

import type { Command } from "./types";

export const steerCommand: Command = {
	name: "steer",
	description: "Queue a steering message (interrupts after current tool calls)",
	execute({ runtime, palette, addNotice }) {
		palette.show({
			mode: "input",
			label: "Steering message",
			inputValue: "",
			onSubmit: (text) => {
				if (!text.trim()) {
					addNotice("info", "/steer cancelled", []);
					return;
				}
				runtime.sendSteer(text.trim()).catch((err) => {
					addNotice("error", "/steer failed", [err instanceof Error ? err.message : String(err)]);
				});
				addNotice("info", "Steering queued", [`"${text.trim().slice(0, 60)}${text.trim().length > 60 ? "…" : ""}"`]);
				palette.pop();
			},
		});
	},
};

export const followUpCommand: Command = {
	name: "followup",
	description: "Queue a follow-up message (processed after agent finishes)",
	execute({ runtime, palette, addNotice }) {
		palette.show({
			mode: "input",
			label: "Follow-up message",
			inputValue: "",
			onSubmit: (text) => {
				if (!text.trim()) {
					addNotice("info", "/followup cancelled", []);
					return;
				}
				runtime.sendFollowUp(text.trim()).catch((err) => {
					addNotice("error", "/followup failed", [err instanceof Error ? err.message : String(err)]);
				});
				addNotice("info", "Follow-up queued", [`"${text.trim().slice(0, 60)}${text.trim().length > 60 ? "…" : ""}"`]);
				palette.pop();
			},
		});
	},
};
