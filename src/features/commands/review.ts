import { createComponent } from "solid-js";
import { resolveDiffSettings } from "../../settings";
import { ReviewContent } from "../review/ReviewContent";
import type { Command } from "./types";

export const codeReviewCommand: Command = {
	name: "code-review",
	description: "Review the current changes",
	async execute({ openCustomOverlay, attachments, toast, runtime }) {
		await openCustomOverlay<void>((props) =>
			createComponent(ReviewContent, {
				onClose: () => props.done(),
				attachments,
				toast,
				openCustomOverlay,
				defaultDiffView: resolveDiffSettings(runtime.settings.diffs).view,
				surfaceProps: props.surfaceProps,
			}),
		);
	},
};
