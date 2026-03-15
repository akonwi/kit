import type { Command } from "./types";
import { formatSessionOption } from "./utils";

export const switchCommand: Command = {
	name: "/switch",
	description: "Switch to another session",
	async execute({ runtime, palette }) {
		const sessions = await runtime.listAllSessions();
		if (sessions.length === 0) return;

		const sorted = [...sessions].sort(
			(a, b) => b.modified.getTime() - a.modified.getTime(),
		);

		palette.show({
			filterable: true,
			options: sorted.map((s) => {
				const { label, description } = formatSessionOption(s);
				return {
					name: label,
					description,
					value: s,
					action: async (ctx) => {
						try {
							await runtime.switchSession(s.path);
						} catch (error) {
							console.error(error);
						}
						ctx.dismiss();
					},
				};
			}),
		});
	},
};
