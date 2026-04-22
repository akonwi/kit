import { createComponent } from "solid-js";
import { ReviewContent } from "../review/ReviewContent";
import type { Command } from "./types";

export const diffCommand: Command = {
	name: "diff",
	description: "View the current uncommitted diff in a terminal modal",
	async execute({ openCustomOverlay }) {
		await openCustomOverlay<void>((props) =>
			createComponent(ReviewContent, {
				onClose: () => props.done(),
			}),
		);
	},
};
