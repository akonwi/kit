import type { InternalPluginAPI } from "../../plugins";
import type { Settings } from "../../settings";
import { resolveAndApplyTheme } from "../../shell/theme";
import { listUserThemes } from "../../shell/themes/loader";
import { discoverSpeechVoices } from "../notifications/voices";
import { SettingsContent } from "./SettingsContent";

async function persistSettings(
	kit: InternalPluginAPI,
	settings: Settings,
): Promise<void> {
	await kit.settings.update(settings);
	await resolveAndApplyTheme(settings.theme ?? "system");
}

export function SettingsPlugin(kit: InternalPluginAPI): void {
	const speechVoicesPromise = discoverSpeechVoices();

	kit.registerCommand(
		"settings",
		{ description: "Open application settings" },
		async () => {
			const [speechVoices, userThemes] = await Promise.all([
				speechVoicesPromise,
				listUserThemes(),
			]);
			await kit.ui.custom((props) => (
				<SettingsContent
					initialSettings={kit.settings.get()}
					speechVoices={speechVoices}
					userThemes={userThemes}
					onSave={(settings) => persistSettings(kit, settings)}
					onClose={() => props.done(undefined)}
					surfaceProps={props.surfaceProps}
				/>
			));
		},
	);
}
