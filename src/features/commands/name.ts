// @ts-nocheck — disabled pending rewrite
import type { Command } from "./types";

export const nameCommand: Command = {
	name: "name",
	description: "Set session display name",
	execute({ runtime, palette }) {
		const currentName = runtime.getSession().sessionName || "";

		palette.show({
			mode: "input",
			label: "Session name",
			inputValue: currentName,
			onSubmit: (value, ctx) => {
				if (value.trim()) {
					runtime.setSessionName(value.trim());
				}
				ctx.dismiss();
			},
		});
	},
};
