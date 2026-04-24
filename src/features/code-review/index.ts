import { Plugin } from "../../plugins/Plugin";
import type { Command } from "../commands/types";
import { CodeReviewAttachment } from "./attachment";
import { codeReviewBrowserHost } from "./browser-host";

const codeReviewCommand: Command = {
	name: "code-review",
	description: "Open the browser-backed code review prototype",
	async execute({ runtime }) {
		try {
			await codeReviewBrowserHost.launch(runtime);
		} catch (error) {
			runtime.emitError("Code review failed", [String(error)]);
		}
	},
};

export class CodeReviewPlugin extends Plugin {
	initialize(): void {
		this.registerCommand(codeReviewCommand);
		void codeReviewBrowserHost.activate(this.ctx.runtime).catch((error) => {
			console.warn("[code-review] failed to activate browser host", error);
		});
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
