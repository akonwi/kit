import { expect, test } from "bun:test";
import { testRender } from "@opentui/solid";
import { createComponent } from "solid-js";
import type { Settings } from "../../settings";
import { ChoiceSettingsRow } from "./ChoiceSettingsRow";
import type {
	ModelOverrideEdit,
	SettingsContextValue,
} from "./SettingsContext";
import { SettingsProvider, useSettingsContext } from "./SettingsContext";
import type { SettingsRowData } from "./SettingsTypes";

const MODEL_OPTIONS = [
	{
		label: "Claude Sonnet 4.5",
		selector: "anthropic/claude-sonnet-4-5",
		description: "anthropic",
	},
];

type Harness = {
	context: SettingsContextValue;
	saved: Settings[];
	setup: Awaited<ReturnType<typeof testRender>>;
};

async function renderSettings(options: {
	initialSettings: Settings;
	onEditModelOverride?: (
		currentOverrides: Settings["modelOverrides"],
	) => Promise<ModelOverrideEdit | undefined>;
}): Promise<Harness> {
	const saved: Settings[] = [];
	let context: SettingsContextValue | undefined;

	function Capture() {
		context = useSettingsContext();
		return (
			<ChoiceSettingsRow
				row={
					context
						.rows()
						.find((row) => row.id === "modelOverrides") as SettingsRowData & {
						kind: "choice";
					}
				}
				index={1}
			/>
		);
	}

	const setup = await testRender(
		() =>
			createComponent(SettingsProvider, {
				initialSettings: options.initialSettings,
				modelOptions: MODEL_OPTIONS,
				onSelectDefaultModel: async () => undefined,
				onEditModelOverride:
					options.onEditModelOverride ?? (async () => undefined),
				onSave: async (settings) => {
					saved.push(settings);
				},
				get children() {
					return createComponent(Capture, {});
				},
			}),
		{ width: 60, height: 6 },
	);
	await setup.renderOnce();
	if (!context) throw new Error("settings context was not captured");
	return { context, saved, setup };
}

test("shows the Model Context Windows row with a None summary and chevron", async () => {
	const { context, setup } = await renderSettings({ initialSettings: {} });
	try {
		const row = context
			.rows()
			.find((candidate) => candidate.id === "modelOverrides");
		expect(row).toEqual({
			id: "modelOverrides",
			kind: "choice",
			label: "Model Context Windows",
			help: "Per-model contextWindow overrides.",
			value: "None",
		});
		await setup.renderOnce();
		const frame = setup.captureCharFrame();
		expect(frame).toContain("None ›");
	} finally {
		setup.renderer.destroy();
	}
});

test("summarizes configured overrides in the row value", async () => {
	const { context, setup } = await renderSettings({
		initialSettings: {
			modelOverrides: {
				"anthropic/claude-sonnet-4-5": { contextWindow: 8192 },
			},
		},
	});
	try {
		const row = context
			.rows()
			.find((candidate) => candidate.id === "modelOverrides");
		expect(row?.kind === "choice" && row.value).toBe("1 override");
	} finally {
		setup.renderer.destroy();
	}
});

test("activating the row persists a new contextWindow override", async () => {
	const { context, saved, setup } = await renderSettings({
		initialSettings: {},
		onEditModelOverride: async () => ({
			selector: "anthropic/claude-sonnet-4-5",
			contextWindow: 8192,
		}),
	});
	try {
		const index = context
			.rows()
			.findIndex((row) => row.id === "modelOverrides");
		await context.actions.activateRow(index);
		expect(saved).toEqual([
			{
				modelOverrides: {
					"anthropic/claude-sonnet-4-5": { contextWindow: 8192 },
				},
			},
		]);
	} finally {
		setup.renderer.destroy();
	}
});

test("clearing the last override removes modelOverrides entirely", async () => {
	const { context, saved, setup } = await renderSettings({
		initialSettings: {
			modelOverrides: {
				"anthropic/claude-sonnet-4-5": { contextWindow: 8192 },
			},
		},
		onEditModelOverride: async () => ({
			selector: "anthropic/claude-sonnet-4-5",
			contextWindow: null,
		}),
	});
	try {
		const index = context
			.rows()
			.findIndex((row) => row.id === "modelOverrides");
		await context.actions.activateRow(index);
		expect(saved).toEqual([{}]);
	} finally {
		setup.renderer.destroy();
	}
});
