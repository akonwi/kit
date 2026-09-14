import type { Accessor, JSX } from "solid-js";
import {
	createComponent,
	createContext,
	createMemo,
	createSignal,
	useContext,
} from "solid-js";
import type { Settings } from "../../settings";
import type {
	BooleanSettingsRowData,
	SettingsModelOption,
	SettingsRowData,
} from "./SettingsTypes";

export type SettingsContextValue = {
	focusedRowIndex: Accessor<number>;
	error: Accessor<string | null>;
	rows: Accessor<SettingsRowData[]>;
	isRowFocused: (index: number) => boolean;
	actions: {
		toggleBoolean: (field: BooleanSettingsRowData["id"]) => Promise<void>;
		focusRow: (index: number) => void;
		activateRow: (index?: number) => Promise<void>;
	};
};

type SettingsProviderProps = {
	initialSettings: Settings;
	modelOptions: SettingsModelOption[];
	onSelectDefaultModel: (
		currentSelector: string | undefined,
	) => Promise<string | null | undefined>;
	onEditModelOverride: (
		currentOverrides: Settings["modelOverrides"],
	) => Promise<ModelOverrideEdit | undefined>;
	onSave: (settings: Settings) => Promise<void>;
	children: JSX.Element;
};

/** A single edit to modelOverrides: null contextWindow clears the override. */
export type ModelOverrideEdit = {
	selector: string;
	contextWindow: number | null;
};

const SettingsContext = createContext<SettingsContextValue>();

export function useSettingsContext(): SettingsContextValue {
	const ctx = useContext(SettingsContext);
	if (!ctx) {
		throw new Error("Settings components must be used inside SettingsProvider");
	}
	return ctx;
}

function cloneSettings(settings: Settings): Settings {
	return {
		...settings,
		diffs:
			typeof settings.diffs === "object" && settings.diffs !== null
				? { ...settings.diffs }
				: settings.diffs,
		retry:
			typeof settings.retry === "object" && settings.retry !== null
				? { ...settings.retry }
				: settings.retry,
		...(settings.modelOverrides
			? {
					modelOverrides: Object.fromEntries(
						Object.entries(settings.modelOverrides).map(
							([selector, override]) => [selector, { ...override }],
						),
					),
				}
			: {}),
	};
}

export function SettingsProvider(props: SettingsProviderProps) {
	const [settings, setSettings] = createSignal<Settings>(
		cloneSettings(props.initialSettings),
	);
	const [focusedRowIndex, setFocusedRowIndex] = createSignal(0);
	const [error, setError] = createSignal<string | null>(null);
	let selectingModel = false;
	let editingOverride = false;

	const rows = createMemo<SettingsRowData[]>(() => {
		const current = settings();
		const configuredModel = current.defaultModel;
		const configuredLabel = configuredModel
			? (props.modelOptions.find(
					(option) => option.selector === configuredModel,
				)?.label ?? configuredModel)
			: "Automatic";
		const overrideCount = Object.keys(current.modelOverrides ?? {}).length;
		const overridesSummary =
			overrideCount === 0
				? "None"
				: overrideCount === 1
					? "1 override"
					: `${overrideCount} overrides`;
		return [
			{
				id: "defaultModel",
				kind: "choice",
				label: "Default Model",
				help: "Used when starting a new session.",
				value: configuredLabel,
			},
			{
				id: "modelOverrides",
				kind: "choice",
				label: "Model Context Windows",
				help: "Per-model contextWindow overrides.",
				value: overridesSummary,
			},
			{
				id: "sessionNaming",
				kind: "boolean",
				label: "Auto-name Sessions",
				help: "Generated after the first couple of turns.",
				checked: current.sessionNaming !== false,
			},
			{
				id: "pager",
				kind: "boolean",
				label: "Auto-open Pager",
				help: "A paged modal UX for long agent responses",
				checked: current.pager !== false,
			},
		];
	});

	async function persist(nextSettings: Settings): Promise<boolean> {
		try {
			await props.onSave(nextSettings);
			setSettings(cloneSettings(nextSettings));
			setError(null);
			return true;
		} catch (cause) {
			const message =
				cause instanceof Error
					? cause.message
					: String(cause || "Unknown error");
			setError(`Failed to save settings: ${message}`);
			return false;
		}
	}

	async function toggleBoolean(
		rowId: BooleanSettingsRowData["id"],
	): Promise<void> {
		const current = cloneSettings(settings());
		const next =
			rowId === "sessionNaming"
				? { ...current, sessionNaming: current.sessionNaming === false }
				: { ...current, pager: current.pager === false };
		await persist(next);
	}

	async function selectDefaultModel(): Promise<void> {
		if (selectingModel) return;
		selectingModel = true;
		try {
			const current = cloneSettings(settings());
			const selection = await props.onSelectDefaultModel(current.defaultModel);
			if (selection === undefined) return;
			await persist({ ...current, defaultModel: selection ?? undefined });
		} finally {
			selectingModel = false;
		}
	}

	async function editModelOverride(): Promise<void> {
		if (editingOverride) return;
		editingOverride = true;
		try {
			const current = cloneSettings(settings());
			const edit = await props.onEditModelOverride(current.modelOverrides);
			if (edit === undefined) return;
			const overrides = { ...(current.modelOverrides ?? {}) };
			if (edit.contextWindow === null) {
				delete overrides[edit.selector];
			} else {
				overrides[edit.selector] = { contextWindow: edit.contextWindow };
			}
			const next: Settings = { ...current };
			if (Object.keys(overrides).length > 0) {
				next.modelOverrides = overrides;
			} else {
				delete next.modelOverrides;
			}
			await persist(next);
		} finally {
			editingOverride = false;
		}
	}

	function focusRow(index: number): void {
		const max = Math.max(0, rows().length - 1);
		setFocusedRowIndex(Math.max(0, Math.min(index, max)));
	}

	async function activateRow(index = focusedRowIndex()): Promise<void> {
		const row = rows()[index];
		if (!row || row.disabled) return;
		if (row.kind === "choice") {
			if (row.id === "modelOverrides") {
				await editModelOverride();
				return;
			}
			await selectDefaultModel();
			return;
		}
		await toggleBoolean(row.id);
	}

	const value: SettingsContextValue = {
		focusedRowIndex,
		error,
		rows,
		isRowFocused: (index) => focusedRowIndex() === index,
		actions: { toggleBoolean, focusRow, activateRow },
	};

	// createComponent with a lazy children getter matches the Solid compiler
	// output and keeps context propagation working under eager JSX runtimes.
	return createComponent(SettingsContext.Provider, {
		value,
		get children() {
			return props.children;
		},
	});
}
