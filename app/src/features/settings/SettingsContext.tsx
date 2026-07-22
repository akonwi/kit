import type { Accessor, JSX } from "solid-js";
import { createContext, createMemo, createSignal, useContext } from "solid-js";
import type { Settings } from "../../settings";
import type { BooleanSettingsRowData, SettingsRowData } from "./SettingsTypes";

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
	onSave: (settings: Settings) => Promise<void>;
	children: JSX.Element;
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
	};
}

export function SettingsProvider(props: SettingsProviderProps) {
	const [settings, setSettings] = createSignal<Settings>(
		cloneSettings(props.initialSettings),
	);
	const [focusedRowIndex, setFocusedRowIndex] = createSignal(0);
	const [error, setError] = createSignal<string | null>(null);

	const rows = createMemo<SettingsRowData[]>(() => {
		const current = settings();
		return [
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

	function focusRow(index: number): void {
		const max = Math.max(0, rows().length - 1);
		setFocusedRowIndex(Math.max(0, Math.min(index, max)));
	}

	async function activateRow(index = focusedRowIndex()): Promise<void> {
		const row = rows()[index];
		if (!row || row.disabled) return;
		await toggleBoolean(row.id);
	}

	const value: SettingsContextValue = {
		focusedRowIndex,
		error,
		rows,
		isRowFocused: (index) => focusedRowIndex() === index,
		actions: { toggleBoolean, focusRow, activateRow },
	};

	return (
		<SettingsContext.Provider value={value}>
			{props.children}
		</SettingsContext.Provider>
	);
}
