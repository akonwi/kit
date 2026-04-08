import type { PaletteContext } from "../../state/palette";
import type { Command } from "./types";

export const nameCommand: Command = {
	name: "name",
	description: "Set session display name",
	execute({ runtime, palette }) {
		const currentName = runtime.getSession().name ?? "";

		palette.show({
			mode: "input",
			label: "Session name",
			inputValue: currentName,
			onSubmit: (value: string, ctx: PaletteContext) => {
				void runtime.setSessionName(value.trim());
				ctx.dismiss();
			},
		});
	},
};
