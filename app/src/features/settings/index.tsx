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
		"zen",
		{ description: "Toggle minimal transcript mode" },
		async () => {
			const enabled = kit.settings.get().zen === true;
			await kit.settings.update({ zen: !enabled });
			kit.ui.toast({
				title: !enabled ? "Zen transcript enabled" : "Zen transcript disabled",
				variant: "info",
			});
		},
	);

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
