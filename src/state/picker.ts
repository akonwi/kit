/**
 * Picker — a stack-based overlay that can show lists or input prompts.
 * Only the top entry renders. Actions control their own lifecycle via ctx.dismiss().
 */

export type PickerContext = {
	/** Pop this picker from the stack */
	dismiss: () => void;
};

export type PickerOption = {
	name: string;
	description: string;
	argHint?: string;
	value?: unknown;
	action: (ctx: PickerContext) => void;
};

export type PickerKeyBinding = (
	option: PickerOption,
	ctx: PickerContext,
) => void;

export type PickerFilterChangeResult =
	| boolean
	| {
			query?: string;
			options?: PickerOption[];
			selectedIndex?: number;
	  }
	| undefined;

export type PickerConfig =
	| {
			options: PickerOption[];
			filterable?: boolean;
			hint?: string;
			/** Called when the picker entry is removed from the stack (escape, dismiss, or pop). */
			onDismiss?: () => void;
			/** Called when the filter text changes. Return false to intercept; or override query/options. */
			onFilterChange?: (text: string) => PickerFilterChangeResult;
	  }
	| {
			mode: "input";
			label?: string;
			inputValue?: string;
			onSubmit: (value: string, ctx: PickerContext) => void;
			/** Called when the picker entry is removed from the stack (escape, dismiss, or pop). */
			onDismiss?: () => void;
	  };

/** Internal entry stored on the stack */
export type PickerEntry = {
	id: number;
	onDismiss?: () => void;
	onFilterChange?: (text: string) => PickerFilterChangeResult;
} & (
	| {
			mode: "list";
			options: PickerOption[];
			allOptions: PickerOption[];
			selectedIndex: number;
			filterable: boolean;
			filterText: string;
			hint: string;
			keyBindings: Record<string, PickerKeyBinding>;
	  }
	| {
			mode: "input";
			label: string;
			inputValue: string;
			onSubmit: (value: string, ctx: PickerContext) => void;
	  }
);

/** Derived view for rendering — strips functions and internal state from entries */
export type PickerSnapshot = {
	visible: boolean;
	mode: "list" | "input";
	// list mode
	options: Array<{ name: string; description: string; argHint?: string }>;
	selectedIndex: number;
	filterable: boolean;
	filterText: string;
	hint: string;
	// input mode
	label: string;
	inputValue: string;
};

export const emptySnapshot: PickerSnapshot = {
	visible: false,
	mode: "list",
	options: [],
	selectedIndex: 0,
	filterable: false,
	filterText: "",
	hint: "",
	label: "",
	inputValue: "",
};

export function snapshotFromEntry(entry: PickerEntry): PickerSnapshot {
	if (entry.mode === "input") {
		return {
			visible: true,
			mode: "input",
			options: [],
			selectedIndex: 0,
			filterable: false,
			filterText: "",
			hint: "",
			label: entry.label,
			inputValue: entry.inputValue,
		};
	}
	return {
		visible: true,
		mode: "list",
		options: entry.options.map((o) => ({
			name: o.name,
			description: o.description,
			argHint: o.argHint,
		})),
		selectedIndex: entry.selectedIndex,
		filterable: entry.filterable,
		filterText: entry.filterText,
		hint: entry.hint,
		label: "",
		inputValue: "",
	};
}
