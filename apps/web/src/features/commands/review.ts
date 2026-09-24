import type { Command } from "./types";

export const codeReviewCommand: Command = {
	name: "code-review",
	description: "Review the current changes",
	execute({ reviewWorkspace }) {
		reviewWorkspace.open();
	},
};
