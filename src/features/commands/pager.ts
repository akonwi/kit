import type { Command } from "./types";

export const pagerCommand: Command = {
	name: "pager",
	description: "Open pager for last assistant response, or close if open",
	execute(ctx) {
		if (ctx.pager.active) {
			ctx.pager.close();
			return;
		}

		const messages = ctx.runtime.getMessages();
		const activated = ctx.pager.tryActivate(messages);
		if (!activated) {
			ctx.palette.show({
				mode: "modal",
				title: "Pager",
				lines: ["No long assistant response found to paginate."],
			});
		}
	},
};
