import type { PickerContext } from "../../state/picker";
import type { Command } from "./types";

export const nameCommand: Command = {
	name: "name",
	argName: "name",
	description: "Set session display name",
	execute({ runtime, picker, args }) {
		const trimmed = args.trim();
		if (trimmed) {
			void runtime.setSessionName(trimmed);
			return;
		}

		const currentName = runtime.getSession().name ?? "";
		picker.show({
			label: "Session name",
			inputValue: currentName,
			onSubmit: (value: string, ctx: PickerContext) => {
				void runtime.setSessionName(value.trim());
				ctx.dismiss();
			},
		});
	},
};
