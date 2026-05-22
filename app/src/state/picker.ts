/**
 * Picker — a stack-based overlay that can show selectable lists, text prompts,
 * or filterable combinations of both. Only the top entry renders. Actions
 * control their own lifecycle via ctx.dismiss().
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

export type PickerConfig = {
	options?: PickerOption[];
	filterable?: boolean;
	label?: string;
	/** Initial text for the picker input. */
	inputValue?: string;
	/** Called when the current text is accepted. */
	onSubmit?: (value: string, ctx: PickerContext) => void;
	/** Called when the picker entry is removed from the stack (escape, dismiss, or pop). */
	onDismiss?: () => void;
	/** Called when the filter text changes. Return false to intercept; or override query/options. */
	onFilterChange?: (text: string) => PickerFilterChangeResult;
};

/** Internal entry stored on the stack */
export type PickerEntry = {
	id: number;
	onDismiss?: () => void;
	onFilterChange?: (text: string) => PickerFilterChangeResult;
	onSubmit?: (value: string, ctx: PickerContext) => void;
	label: string;
	options: PickerOption[];
	allOptions: PickerOption[];
	selectedIndex: number;
	filterable: boolean;
	filterText: string;
	keyBindings: Record<string, PickerKeyBinding>;
};

/** Derived view for rendering — strips functions and internal state from entries */
export type PickerSnapshot = {
	visible: boolean;
	options: Array<{ name: string; description: string; argHint?: string }>;
	allOptions: Array<{ name: string; description: string; argHint?: string }>;
	selectedIndex: number;
	filterable: boolean;
	filterText: string;
	label: string;
};

export const emptySnapshot: PickerSnapshot = {
	visible: false,
	options: [],
	allOptions: [],
	selectedIndex: 0,
	filterable: false,
	filterText: "",
	label: "",
};

export function snapshotFromEntry(entry: PickerEntry): PickerSnapshot {
	return {
		visible: true,
		options: entry.options.map((o) => ({
			name: o.name,
			description: o.description,
			argHint: o.argHint,
		})),
		allOptions: entry.allOptions.map((o) => ({
			name: o.name,
			description: o.description,
			argHint: o.argHint,
		})),
		selectedIndex: entry.selectedIndex,
		filterable: entry.filterable,
		filterText: entry.filterText,
		label: entry.label,
	};
}
