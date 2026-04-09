/**
 * Command palette — a stack-based overlay that can show lists or input prompts.
 * Only the top entry renders. Actions control their own lifecycle via ctx.dismiss().
 */

export type PaletteContext = {
	/** Pop this palette from the stack */
	dismiss: () => void;
};

export type PaletteOption = {
	name: string;
	description: string;
	argHint?: string;
	value?: unknown;
	action: (ctx: PaletteContext) => void;
};

export type PaletteKeyBinding = (
	option: PaletteOption,
	ctx: PaletteContext,
) => void;

export type PaletteFilterChangeResult =
	| boolean
	| {
			query?: string;
			options?: PaletteOption[];
			selectedIndex?: number;
	  }
	| undefined;

export type PaletteConfig =
	| {
			options: PaletteOption[];
			filterable?: boolean;
			hint?: string;
			/** Called when the palette entry is removed from the stack (escape, dismiss, or pop). */
			onDismiss?: () => void;
			/** Called when the filter text changes. Return false to intercept; or override query/options. */
			onFilterChange?: (text: string) => PaletteFilterChangeResult;
	  }
	| {
			mode: "input";
			label?: string;
			inputValue?: string;
			onSubmit: (value: string, ctx: PaletteContext) => void;
			/** Called when the palette entry is removed from the stack (escape, dismiss, or pop). */
			onDismiss?: () => void;
	  }
	| {
			mode: "modal";
			title: string;
			lines: string[];
			/** Called when the palette entry is removed from the stack (escape, dismiss, or pop). */
			onDismiss?: () => void;
	  };

/** Internal entry stored on the stack */
export type PaletteEntry = {
	id: number;
	onDismiss?: () => void;
	onFilterChange?: (text: string) => PaletteFilterChangeResult;
} & (
	| {
			mode: "list";
			options: PaletteOption[];
			allOptions: PaletteOption[];
			selectedIndex: number;
			filterable: boolean;
			filterText: string;
			hint: string;
			keyBindings: Record<string, PaletteKeyBinding>;
	  }
	| {
			mode: "input";
			label: string;
			inputValue: string;
			onSubmit: (value: string, ctx: PaletteContext) => void;
	  }
	| {
			mode: "modal";
			title: string;
			lines: string[];
	  }
);

/** Derived view for rendering — strips functions and internal state from entries */
export type PaletteSnapshot = {
	visible: boolean;
	mode: "list" | "input" | "modal";
	// list mode
	options: Array<{ name: string; description: string; argHint?: string }>;
	selectedIndex: number;
	filterable: boolean;
	filterText: string;
	hint: string;
	// input mode
	label: string;
	inputValue: string;
	// modal mode
	modalTitle: string;
	modalLines: string[];
};

export const emptySnapshot: PaletteSnapshot = {
	visible: false,
	mode: "list",
	options: [],
	selectedIndex: 0,
	filterable: false,
	filterText: "",
	hint: "",
	label: "",
	inputValue: "",
	modalTitle: "",
	modalLines: [],
};

export function snapshotFromEntry(entry: PaletteEntry): PaletteSnapshot {
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
			modalTitle: "",
			modalLines: [],
		};
	}
	if (entry.mode === "modal") {
		return {
			visible: true,
			mode: "modal",
			options: [],
			selectedIndex: 0,
			filterable: false,
			filterText: "",
			hint: "",
			label: "",
			inputValue: "",
			modalTitle: entry.title,
			modalLines: entry.lines,
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
		modalTitle: "",
		modalLines: [],
	};
}
