import { Plugin } from "../../plugins/Plugin";
import { type Settings, saveSettings } from "../../settings";
import { resolveAndApplyTheme } from "../../shell/theme";
import { listUserThemes } from "../../shell/themes/loader";
import { discoverSpeechVoices } from "../notifications/voices";
import { SettingsContent } from "./SettingsContent";

export class SettingsPlugin extends Plugin {
	private speechVoicesPromise = discoverSpeechVoices();

	override initialize(): void {
		this.registerCommand({
			name: "settings",
			description: "Open application settings",
			execute: async () => {
				const [speechVoices, userThemes] = await Promise.all([
					this.speechVoicesPromise,
					listUserThemes(),
				]);
				await this.ctx.ui.custom((props) => (
					<SettingsContent
						initialSettings={this.ctx.settings.settings}
						speechVoices={speechVoices}
						userThemes={userThemes}
						onSave={(settings) => this.persistSettings(settings)}
						onClose={() => props.done(undefined)}
					/>
				));
			},
		});
	}

	private async persistSettings(settings: Settings): Promise<void> {
		await saveSettings(settings);
		this.ctx.settings.settings = settings;
		this.ctx.runtime.emitSettingsChanged(settings);
		await resolveAndApplyTheme(settings.theme ?? "kit");
	}
}
