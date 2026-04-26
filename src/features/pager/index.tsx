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
			await this.ctx.runtime.submitMessage(msg);
		});

		// Auto-activate pager on turn completion if the assistant response is long.
		// Respects the `pager` setting; `/pager` always works regardless.
		this.subscribeRuntimeEvent("turn.completed", async () => {
			if (this.ctx.settings.settings.pager === false) return;
			if (this.pager.active) return;
			if (this.pager.tryActivate(this.ctx.runtime.getMessages())) {
				await this.openPager();
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
		const component = (props: {
			done: (result: unknown) => void;
			surfaceProps: import("../../app/overlay-ui").OverlaySurfaceProps;
		}) => (
			<PagerContent
				pager={this.pager}
				onClose={() => props.done(undefined)}
				surfaceProps={props.surfaceProps}
			/>
		);
		await this.ctx.ui.custom(component);
	}
}
