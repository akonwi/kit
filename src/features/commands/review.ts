import { createComponent } from "solid-js";
import { ReviewContent } from "../review/ReviewContent";
import type { Command } from "./types";

export const diffCommand: Command = {
	name: "diff",
	description:
		"Review and comment on the current uncommitted diff in a terminal modal",
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
