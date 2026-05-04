import { createComponent } from "solid-js";
import { ReviewContent } from "../review/ReviewContent";
import type { Command } from "./types";

export const codeReviewCommand: Command = {
	name: "code-review",
	description: "Review the current changes",
	async execute({ openCustomOverlay, attachments, toast }) {
		await openCustomOverlay<void>((props) =>
			createComponent(ReviewContent, {
				onClose: () => props.done(),
				attachments,
				toast,
				openCustomOverlay,
				surfaceProps: props.surfaceProps,
			}),
		);
	},
};
