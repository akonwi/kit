// @ts-nocheck — disabled pending rewrite
import type { Command } from "./types";

export const modelCommand: Command = {
	name: "model",
	description: "Select a model",
	execute({ runtime, palette }) {
		const models = runtime.getAvailableModels();
		if (models.length === 0) return;

		palette.show({
			filterable: true,
			options: models.map((m) => ({
				name: m.name,
				description: m.provider,
				value: m,
				action: async (ctx) => {
					try {
						await runtime.setModel(m.provider, m.id);
					} catch (error) {
						console.error(error);
					}
					ctx.dismiss();
				},
			})),
		});
	},
};
