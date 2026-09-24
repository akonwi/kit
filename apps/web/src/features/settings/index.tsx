import type { InternalPluginAPI } from "../../plugins";
import type { Settings } from "../../settings";
import { CHECK } from "../../shell/glyphs";
import { SettingsContent } from "./SettingsContent";
import type { ModelOverrideEdit } from "./SettingsContext";
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

function parseContextWindow(value: string): number | null | undefined {
	const trimmed = value.trim();
	if (trimmed === "") return null;
	if (!/^\d+$/.test(trimmed)) return undefined;
	const parsed = Number.parseInt(trimmed, 10);
	return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

async function editModelOverride(
	kit: InternalPluginAPI,
	models: SettingsModelOption[],
	currentOverrides: Settings["modelOverrides"],
): Promise<ModelOverrideEdit | undefined> {
	const overrides = currentOverrides ?? {};
	const selector = await kit.ui.select<string>({
		title: "Model Context Windows",
		message: "Pick a model to override its contextWindow.",
		filterable: true,
		placeholder: "Filter models",
		options: models.map((model) => {
			const contextWindow = overrides[model.selector]?.contextWindow;
			return {
				label:
					contextWindow !== undefined ? `${model.label} ${CHECK}` : model.label,
				value: model.selector,
				description:
					contextWindow !== undefined
						? `${model.selector} \u00b7 ${contextWindow}`
						: model.selector,
			};
		}),
	});
	if (selector === undefined) return undefined;
	const existing = overrides[selector]?.contextWindow;
	let message = "Positive integer. Leave blank to clear the override.";
	for (;;) {
		const answer = await kit.ui.input({
			title: `Context Window \u00b7 ${selector}`,
			message,
			placeholder: "e.g. 8192",
			initialValue: existing !== undefined ? String(existing) : undefined,
		});
		if (answer === undefined) return undefined;
		const contextWindow = parseContextWindow(answer);
		if (contextWindow !== undefined) return { selector, contextWindow };
		message = "Enter a positive integer, or leave blank to clear the override.";
	}
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
					onEditModelOverride={(currentOverrides) =>
						editModelOverride(kit, models, currentOverrides)
					}
					onSave={(settings) => persistSettings(kit, settings)}
					onClose={() => props.done(undefined)}
					active={props.active}
					surfaceProps={props.surfaceProps}
				/>
			));
		},
	);
}
