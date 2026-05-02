import { codeReviewBrowserHost } from "../code-review/browser-host";
import type { Command } from "./types";

export const codeReviewCommand: Command = {
	name: "code-review",
	description: "Open the browser-backed code review prototype",
	async execute({ runtime, toast }) {
		try {
			await codeReviewBrowserHost.launch(runtime, toast);
		} catch (error) {
			toast({
				title: "Code review failed",
				lines: [String(error)],
				variant: "error",
			});
		}
	},
};
