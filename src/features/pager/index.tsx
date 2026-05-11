import type { PluginAPI } from "../../plugins";
import { PagerContent } from "./PagerContent";
import {
	createPagerController,
	type PagerController,
} from "./pager-controller";

export type { PagerController } from "./pager-controller";

export function PagerPlugin(kit: PluginAPI): () => void {
	const pager: PagerController = createPagerController();

	async function openPager(): Promise<void> {
		await kit.ui.custom((props) => (
			<PagerContent
				pager={pager}
				onClose={() => props.done(undefined)}
				surfaceProps={props.surfaceProps}
			/>
		));
	}

	// Wire pager feedback submission to runtime
	pager.setSubmitCallback(async (msg) => {
		try {
			await kit.session.submitMessage(msg);
		} catch (error) {
			kit.ui.toast({
				title: "Pager feedback failed",
				lines: [error instanceof Error ? error.message : String(error)],
				variant: "error",
			});
		}
	});

	// Auto-activate pager when the last assistant response substantially
	// overflows the visible transcript viewport.
	// Respects the `pager` setting; `/pager` always works regardless.
	kit.on("agent.turn.completed", async () => {
		if (kit.settings.get().pager === false) return;
		if (pager.active) return;
		if (
			pager.tryAutoActivate(
				kit.session.getMessages(),
				kit.ui.getTranscriptViewport(),
			)
		) {
			await openPager();
		}
	});

	// Register /pager command
	kit.registerCommand(
		"pager",
		{ description: "Open pager for last assistant response, or close if open" },
		async () => {
			if (pager.active) {
				pager.close();
				return;
			}
			if (!pager.tryActivate(kit.session.getMessages())) {
				kit.ui.toast({
					title: "No assistant response to paginate.",
					lines: [],
					variant: "warning",
				});
				return;
			}
			await openPager();
		},
	);

	return () => {
		pager.close();
	};
}
