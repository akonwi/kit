import type { PluginAPI } from "@akonwi/kit/plugin";

type ToastVariant = "info" | "warning" | "error";

export default function UiApiDemoPlugin(kit: PluginAPI) {
	kit.registerCommand(
		"plugin-ui-demo",
		{
			title: "Plugin UI demo",
			description: "Exercise plugin select, input, confirm, and toast APIs",
			argName: "initial note",
			category: "plugins",
		},
		async (ctx) => {
			ctx.ui.toast({
				title: "Plugin UI demo",
				subtitle: "Starting select → input → confirm flow.",
				variant: "info",
			});

			const target = await ctx.ui.select({
				title: "Plugin UI demo: string select",
				message: "Pick a target scope. Escape cancels.",
				options: ["Current file", "Open session", "Whole project"],
				filterable: true,
				placeholder: "Filter scopes...",
			});
			if (!target) {
				ctx.ui.toast({
					title: "Plugin UI demo cancelled",
					subtitle: "No target selected.",
					variant: "warning",
				});
				return;
			}

			const variant = await ctx.ui.select<ToastVariant>({
				title: "Plugin UI demo: value select",
				message: "Pick the toast variant to show at the end.",
				options: [
					{ label: "Info", value: "info", description: "Normal notification" },
					{
						label: "Warning",
						value: "warning",
						description: "Cautionary notification",
					},
					{
						label: "Error",
						value: "error",
						description: "Failure notification",
					},
				],
				filterable: true,
				placeholder: "Filter variants...",
			});
			if (!variant) {
				ctx.ui.toast({
					title: "Plugin UI demo cancelled",
					subtitle: "No toast variant selected.",
					variant: "warning",
				});
				return;
			}

			const note = await ctx.ui.input({
				title: "Plugin UI demo: input",
				message: `Target: ${target}. Type a note for the final toast.`,
				placeholder: "Demo note...",
				initialValue: ctx.args,
			});
			if (note === undefined) {
				ctx.ui.toast({
					title: "Plugin UI demo cancelled",
					subtitle: "Input was cancelled.",
					variant: "warning",
				});
				return;
			}

			const shouldShowToast = await ctx.ui.confirm({
				title: "Plugin UI demo: confirm",
				message: `Show a ${variant} toast for ${target}?`,
				confirmLabel: "Show toast",
				cancelLabel: "Cancel",
				defaultValue: true,
			});
			if (!shouldShowToast) {
				ctx.ui.toast({
					title: "Plugin UI demo cancelled",
					subtitle: "Confirmation returned false.",
					variant: "warning",
				});
				return;
			}

			ctx.ui.toast({
				title: "UI demo complete",
				subtitle: `${target} · ${variant} · note=${note || "(empty)"}`,
				variant,
			});
		},
	);
}
