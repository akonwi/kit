import { createComponent } from "solid-js";
import { ReviewContent } from "../review/ReviewContent";
import type { Command } from "./types";

export const reviewCommand: Command = {
	name: "review",
	description: "Review current uncommitted changes with file and hunk notes",
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
