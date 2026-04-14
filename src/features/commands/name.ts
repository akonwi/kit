import type { PaletteContext } from "../../state/palette";
import type { Command } from "./types";

export const nameCommand: Command = {
	name: "name",
	argName: "name",
	description: "Set session display name",
	execute({ runtime, palette, args }) {
		const trimmed = args.trim();
		if (trimmed) {
			void runtime.setSessionName(trimmed);
			return;
		}

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
