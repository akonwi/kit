import type { InternalPluginAPI } from "../../plugins";
import type { Settings } from "../../settings";
import { CHECK } from "../../shell/glyphs";
import { SettingsContent } from "./SettingsContent";
import type { SettingsModelOption } from "./SettingsTypes";

async function persistSettings(
	kit: InternalPluginAPI,
	settings: Settings,
): Promise<void> {
	await kit.settings.update(settings);
}

function modelOptions(kit: InternalPluginAPI): SettingsModelOption[] {
	return kit.model.getAvailable().map((model) => ({
		label: model.name ?? model.id,
		selector: `${model.provider}/${model.id}`,
		description: model.provider,
	}));
}

export function SettingsPlugin(kit: InternalPluginAPI): void {
	kit.registerCommand(
		"settings",
		{ description: "Open application settings" },
		async () => {
			const models = modelOptions(kit);
			await kit.ui.custom((props) => (
				<SettingsContent
					initialSettings={kit.settings.get()}
					modelOptions={models}
					onSelectDefaultModel={async (currentSelector) => {
						return kit.ui.select<string | null>({
							title: "Default Model",
							message: "Used for newly created sessions.",
							filterable: true,
							placeholder: "Filter models",
							options: [
								{
									label:
										currentSelector === undefined
											? `Automatic ${CHECK}`
											: "Automatic",
									value: null,
									description: "Use Kit's automatic model selection",
								},
								...models.map((model) => ({
									label:
										model.selector === currentSelector
											? `${model.label} ${CHECK}`
											: model.label,
									value: model.selector,
									description: model.selector,
								})),
							],
						});
					}}
					onSave={(settings) => persistSettings(kit, settings)}
					onClose={() => props.done(undefined)}
					active={props.active}
					surfaceProps={props.surfaceProps}
				/>
			));
		},
	);
}
