import { codeReviewBrowserHost } from "../code-review/browser-host";
import type { Command } from "./types";

export const codeReviewCommand: Command = {
	name: "code-review",
	description: "Open the browser-backed code review prototype",
	async execute({ runtime }) {
		try {
			await codeReviewBrowserHost.launch(runtime);
		} catch (error) {
			runtime.emitError("Code review failed", [String(error)]);
		}
	},
};
