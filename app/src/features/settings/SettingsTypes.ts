export type BooleanSettingsRowData = {
	id: "sessionNaming" | "pager";
	kind: "boolean";
	label: string;
	help: string;
	checked: boolean;
	disabled?: boolean;
};

export type ChoiceSettingsRowData = {
	id: "defaultModel";
	kind: "choice";
	label: string;
	help: string;
	value: string;
	disabled?: boolean;
};

export type SettingsModelOption = {
	label: string;
	selector: string;
	description: string;
};

export type SettingsRowData = BooleanSettingsRowData | ChoiceSettingsRowData;
