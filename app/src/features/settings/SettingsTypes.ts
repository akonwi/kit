export type SettingsTabId = "general" | "notifications";

export type EditableField = "speech.maxChars" | "speech.voice" | null;

export type BooleanSettingsRowData = {
	id: "sessionNaming" | "pager" | "speech";
	kind: "boolean";
	label: string;
	help: string;
	checked: boolean;
	disabled?: boolean;
};

export type InputSettingsRowData = {
	id: "speech.maxChars";
	kind: "input";
	label: string;
	help: string;
	value: string;
	placeholder?: string;
	disabled?: boolean;
};

export type SelectSettingsRowData = {
	id: "speech.voice";
	kind: "select";
	label: string;
	help: string;
	value: string;
	placeholder?: string;
	disabled?: boolean;
};

export type SettingsRowData =
	| BooleanSettingsRowData
	| InputSettingsRowData
	| SelectSettingsRowData;

export type SettingsSelectOption<T extends string = string> = {
	name: string;
	description: string;
	value: T;
};

export const TABS: Array<{ id: SettingsTabId; label: string }> = [
	{ id: "general", label: "General" },
	{ id: "notifications", label: "Notifications" },
];
