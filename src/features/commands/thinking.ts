import type { ThinkingLevel } from "@mariozechner/pi-agent-core";
import type { Command } from "./types";

const THINKING_LEVELS: ThinkingLevel[] = [
	"off",
	"minimal",
	"low",
	"medium",
	"high",
	"xhigh",
];

export const thinkingCommand: Command = {
	name: "thinking",
	description: "Cycle or set thinking level",
	execute({ runtime, palette }) {
		const current = runtime.getAgentSession().thinkingLevel ?? "off";

		palette.show({
			options: THINKING_LEVELS.map((level) => ({
				name: level,
				description: level === current ? "(current)" : "",
				value: level,
				action: (ctx) => {
					runtime.setThinkingLevel(level);
					ctx.dismiss();
				},
			})),
		});
	},
};
