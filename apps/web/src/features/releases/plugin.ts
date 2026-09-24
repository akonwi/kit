import type {
	InternalPluginAPI,
	InternalPluginDefinition,
} from "../../plugins/types";
import type { ReleasesWorkspaceController } from "./workspace-controller";

export function createReleasesPlugin(options: {
	workspace: ReleasesWorkspaceController;
}): InternalPluginDefinition {
	return function ReleasesPlugin(kit: InternalPluginAPI): () => void {
		function updateHeader(): void {
			const latest = options.workspace.getState().latest;
			if (!latest) {
				kit.header.clear("update");
				return;
			}
			const tokens = kit.ui.theme().tokens;
			kit.header.set(
				"update",
				[
					kit.ui.text(`Update available: v${latest.version}`, {
						fg: tokens.warningText,
					}),
				],
				{
					side: "right",
					onClick: () => options.workspace.open(),
				},
			);
		}

		kit.registerCommand(
			"release-notes",
			{ description: "Browse Kit release notes" },
			async (ctx) => {
				if (ctx.args.trim()) {
					ctx.ui.toast({
						title: "Release notes",
						subtitle: "Use /release-notes with no arguments.",
						variant: "warning",
					});
					return;
				}
				options.workspace.open();
			},
		);

		const unsubscribe = options.workspace.subscribe(updateHeader);
		updateHeader();
		void options.workspace.checkForUpdate();

		return () => {
			unsubscribe();
			kit.header.clear("update");
		};
	};
}
