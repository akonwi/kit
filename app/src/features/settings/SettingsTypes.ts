export type BooleanSettingsRowData = {
	id: "sessionNaming" | "pager";
	kind: "boolean";
	label: string;
	help: string;
	checked: boolean;
	disabled?: boolean;
};

export type SettingsRowData = BooleanSettingsRowData;
