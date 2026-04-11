import { Plugin } from "../../plugins/Plugin";
import type { CommandContext } from "../commands/types";
import { PagerContent } from "./PagerContent";
import {
	createPagerController,
	type PagerController,
} from "./pager-controller";

export type { PagerController } from "./pager-controller";

export class PagerPlugin extends Plugin {
	private readonly pager: PagerController = createPagerController();

	override initialize(): void {
		// Wire pager feedback submission to runtime
		this.pager.setSubmitCallback(async (msg) => {
			await this.ctx.runtime.submitUserMessage(msg);
		});

		// Auto-activate pager on turn_complete if the assistant response is long
		this.subscribeRuntime(async (event) => {
			if (event.type === "turn_complete") {
				if (this.pager.active) return;
				if (this.pager.tryActivate(this.ctx.runtime.getMessages())) {
					await this.openPager();
				}
			}
		});

		// Register /pager command
		this.registerCommand({
			name: "pager",
			description: "Open pager for last assistant response, or close if open",
			execute: async (ctx: CommandContext) => {
				if (this.pager.active) {
					this.pager.close();
					return;
				}
				if (!this.pager.tryActivate(ctx.runtime.getMessages())) {
					this.ctx.ui.notify(
						"No long assistant response to paginate.",
						"warning",
					);
					return;
				}
				await this.openPager();
			},
		});
	}

	override dispose(): void {
		this.pager.close();
		super.dispose();
	}

	private async openPager(): Promise<void> {
		const component = (props: { done: (result: unknown) => void }) => (
			<PagerContent pager={this.pager} onClose={() => props.done(undefined)} />
		);
		await this.ctx.ui.custom(component);
	}
}
