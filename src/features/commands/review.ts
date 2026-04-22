import { createComponent } from "solid-js";
import { ReviewContent } from "../review/ReviewContent";
import type { Command } from "./types";

export const reviewCommand: Command = {
	name: "code-review",
	description: "Review current uncommitted changes in a code review modal",
	async execute({ openCustomOverlay, runtime }) {
		await openCustomOverlay<void>((props) =>
			createComponent(ReviewContent, {
				onClose: () => props.done(),
				onSubmit: async (message: string) => {
					await runtime.submitUserMessage(message);
				},
			}),
		);
	},
};
