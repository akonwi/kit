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
			try {
				await this.ctx.runtime.submitMessage(msg);
			} catch (error) {
				this.ctx.ui.toast({
					title: "Pager feedback failed",
					lines: [error instanceof Error ? error.message : String(error)],
					variant: "error",
				});
			}
		});

		// Auto-activate pager when the last assistant response substantially
		// overflows the visible transcript viewport.
		// Respects the `pager` setting; `/pager` always works regardless.
		this.subscribeRuntimeEvent("agent.turn.completed", async () => {
			if (this.ctx.settings.settings.pager === false) return;
			if (this.pager.active) return;
			if (
				this.pager.tryAutoActivate(
					this.ctx.runtime.getMessages(),
					this.ctx.ui.getTranscriptViewport(),
				)
			) {
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
					this.ctx.ui.toast({
						title: "No assistant response to paginate.",
						lines: [],
						variant: "warning",
					});
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
