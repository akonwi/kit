import { loadSettingsSync, saveSettings } from "../../settings";
import { CHECK } from "../../shell/glyphs";
import { resolveAndApplyTheme } from "../../shell/theme";
import { listUserThemes } from "../../shell/themes/loader";
import type { PickerContext } from "../../state/picker";
import type { Command } from "./types";

export const themeCommand: Command = {
	name: "theme",
	description: "Switch the color theme",
	async execute({ runtime, picker, toast }) {
		const userThemes = (await listUserThemes()).sort((a, b) =>
			a.localeCompare(b),
		);
		const { settings } = loadSettingsSync();
		const originalTheme = settings.theme ?? "system";

		const themeOptions = [
			{ name: "System", value: "system" },
			...userThemes.map((t) => ({
				name: t.charAt(0).toUpperCase() + t.slice(1),
				value: t,
			})),
		];

		const initialIndex = Math.max(
			0,
			themeOptions.findIndex((opt) => opt.value === originalTheme),
		);

		// Latest-wins token to prevent stale async theme resolutions
		let previewGeneration = 0;
		let confirmed = false;

		await new Promise<void>((resolve) => {
			picker.show({
				filterable: true,
				label: "Theme",
				selectedIndex: initialIndex,
				onSelectionChange: (option) => {
					const value = option.value as string;
					const gen = ++previewGeneration;
					void resolveAndApplyTheme(value).catch(() => {
						// Stale or failed preview — ignore
						if (gen === previewGeneration) previewGeneration++;
					});
				},
				onDismiss: () => {
					if (!confirmed) {
						++previewGeneration;
						void resolveAndApplyTheme(originalTheme).catch(() => {});
					}
					resolve();
				},
				options: themeOptions.map((opt) => ({
					name: opt.value === originalTheme ? `${opt.name} ${CHECK}` : opt.name,
					description: opt.value === originalTheme ? "Current" : "",
					value: opt.value,
					action: async (ctx: PickerContext) => {
						++previewGeneration;
						try {
							const latest = loadSettingsSync();
							const next = { ...latest.settings, theme: opt.value };
							await saveSettings(next);
							await resolveAndApplyTheme(opt.value);
							runtime.emitSettingsChanged(next);
							confirmed = true;
						} catch (e) {
							// Restore original on failure
							void resolveAndApplyTheme(originalTheme).catch(() => {});
							toast({
								title: "Failed to apply theme",
								subtitle: e instanceof Error ? e.message : String(e),
								variant: "error",
							});
						}
						ctx.dismiss();
					},
				})),
			});
		});
	},
};
