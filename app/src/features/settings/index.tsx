import type { InternalPluginAPI } from "../../plugins";
import type { Settings } from "../../settings";
import { discoverSpeechVoices } from "../notifications/voices";
import { SettingsContent } from "./SettingsContent";

async function persistSettings(
	kit: InternalPluginAPI,
	settings: Settings,
): Promise<void> {
	await kit.settings.update(settings);
}

export function SettingsPlugin(kit: InternalPluginAPI): void {
	const speechVoicesPromise = discoverSpeechVoices();

	kit.registerCommand(
		"settings",
		{ description: "Open application settings" },
		async () => {
			const speechVoices = await speechVoicesPromise;
			await kit.ui.custom((props) => (
				<SettingsContent
					initialSettings={kit.settings.get()}
					speechVoices={speechVoices}
					onSave={(settings) => persistSettings(kit, settings)}
					onClose={() => props.done(undefined)}
					surfaceProps={props.surfaceProps}
				/>
			));
		},
	);
}
