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
  value?: unknown;
  action: (ctx: PaletteContext) => void;
};

export type PaletteKeyBinding = (
  option: PaletteOption,
  ctx: PaletteContext,
) => void;

export type PaletteConfig = {
  options: PaletteOption[];
  filterable?: boolean;
  hint?: string;
} | {
  mode: "input";
  label?: string;
  inputValue?: string;
  onSubmit: (value: string, ctx: PaletteContext) => void;
};

/** Internal entry stored on the stack */
export type PaletteEntry = {
  id: number;
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
);

/** Serializable snapshot for rendering (no functions in Solid store) */
export type PaletteSnapshot = {
  visible: boolean;
  mode: "list" | "input";
  // list mode
  options: Array<{ name: string; description: string }>;
  selectedIndex: number;
  filterable: boolean;
  filterText: string;
  hint: string;
  // input mode
  label: string;
  inputValue: string;
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
    };
  }
  return {
    visible: true,
    mode: "list",
    options: entry.options.map((o) => ({ name: o.name, description: o.description })),
    selectedIndex: entry.selectedIndex,
    filterable: entry.filterable,
    filterText: entry.filterText,
    hint: entry.hint,
    label: "",
    inputValue: "",
  };
}
