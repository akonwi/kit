import { Plugin } from "../../plugins/Plugin";
import { CodeReviewAttachment } from "./attachment";
import { codeReviewBrowserHost } from "./browser-host";
import { resetCodeReviewStatus, updateCodeReviewStatus } from "./state";

export class CodeReviewPlugin extends Plugin {
	initialize(): void {
		let lastEmittedError: string | null = null;
		this.registerCommand({
			name: "code-review",
			description: "Review + comment on the current diff",
			execute: async ({ runtime }) => {
				try {
					await codeReviewBrowserHost.launch(runtime, this.ctx.ui.toast);
				} catch {
					// Host status subscription emits the user-facing error.
				}
			},
		});
		this.addDisposer(
			codeReviewBrowserHost.subscribeStatus((status) => {
				updateCodeReviewStatus(status);
				if (status.serverState === "error" && status.lastError) {
					if (status.lastError !== lastEmittedError) {
						lastEmittedError = status.lastError;
						this.ctx.ui.toast({
							title: "Code review failed",
							lines: [status.lastError],
							variant: "error",
						});
					}
					return;
				}
				if (!status.lastError) lastEmittedError = null;
			}),
		);
		codeReviewBrowserHost.setOnReviewSubmitted((review) => {
			this.ctx.attachments.attach(
				new CodeReviewAttachment("code-review", review),
			);
		});
		this.addDisposer(() => {
			codeReviewBrowserHost.setOnReviewSubmitted(null);
		});
		this.addDisposer(() => {
			codeReviewBrowserHost.dispose();
			resetCodeReviewStatus();
		});
		this.addDisposer(
			this.ctx.attachments.subscribe((event) => {
				if (event.type === "detached" && event.id === "code-review") {
					codeReviewBrowserHost.clearPendingReview();
				}
			}),
		);
	}
}
