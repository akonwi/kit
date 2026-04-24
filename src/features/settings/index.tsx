import { Plugin } from "../../plugins/Plugin";
import { type Settings, saveSettings } from "../../settings";
import { SettingsContent } from "./SettingsContent";

export class SettingsPlugin extends Plugin {
	override initialize(): void {
		this.registerCommand({
			name: "settings",
			description: "Open application settings",
			execute: async () => {
				await this.ctx.ui.custom((props) => (
					<SettingsContent
						initialSettings={this.ctx.settings.settings}
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
	}
}
