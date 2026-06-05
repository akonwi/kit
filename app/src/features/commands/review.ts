import { createComponent } from "solid-js";
import type { ReviewDiffView } from "../../settings";
import {
	loadSettingsSync,
	resolveDiffSettings,
	saveSettings,
} from "../../settings";
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
				defaultDiffView: resolveDiffSettings(runtime.settings.diffs).view,
				onDiffViewChanged: (view: ReviewDiffView) => {
					const { settings } = loadSettingsSync();
					const next = { ...settings, diffs: { view } };
					void saveSettings(next)
						.then(() => runtime.emitSettingsChanged(next))
						.catch((e) => {
							toast({
								title: "Failed to save diff view",
								subtitle: e instanceof Error ? e.message : String(e),
								variant: "error",
							});
						});
				},
				surfaceProps: props.surfaceProps,
			}),
		);
	},
};
