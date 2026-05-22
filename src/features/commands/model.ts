import { CHECK } from "../../shell/glyphs";
import type { PickerContext } from "../../state/picker";
import type { Command } from "./types";

export const modelCommand: Command = {
	name: "model",
	description: "Switch the active model",
	async execute({ runtime, picker }) {
		const models = runtime.getAvailableModels();
		const currentId = runtime.getCurrentModelId();

		await new Promise<void>((resolve) => {
			let selected: (typeof models)[0] | null = null;
			picker.show({
				filterable: true,
				onDismiss: () => {
					if (selected) {
						try {
							runtime.setModel(selected);
						} catch (e) {
							console.log("[model] setModel error:", e);
						}
					}
					resolve();
				},
				options: models.map((m) => ({
					name:
						m.id === currentId
							? `${m.name ?? m.id} ${CHECK}`
							: (m.name ?? m.id),
					description: m.provider,
					value: m,
					action: (ctx: PickerContext) => {
						selected = m;
						ctx.dismiss();
					},
				})),
			});
		});
	},
};
