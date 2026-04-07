import type { Command } from "./types";

export const toastTestCommand: Command = {
	name: "toast-test",
	description: "Fire test toasts (info + error)",
	execute({ runtime }) {
		runtime.emitInfo("Info toast", ["This is an info message"]);
		setTimeout(() => {
			runtime.emitError("Error toast", ["This is an error message"]);
		}, 500);
	},
};
