export type SettingsTabId = "general" | "notifications";

export type EditableField =
	| "speech.maxChars"
	| "speech.voice"
	| "retry.maxRetries"
	| "retry.baseDelayMs"
	| "retry.maxDelayMs"
	| null;

export type BooleanSettingsRowData = {
	id:
		| "guidedQuestions"
		| "sessionNaming"
		| "pager"
		| "speech"
		| "retry.enabled";
	kind: "boolean";
	label: string;
	help: string;
	checked: boolean;
	disabled?: boolean;
};

export type InputSettingsRowData = {
	id:
		| "speech.maxChars"
		| "retry.maxRetries"
		| "retry.baseDelayMs"
		| "retry.maxDelayMs";
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
