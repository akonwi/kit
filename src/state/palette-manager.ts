import type { SetStoreFunction } from "solid-js/store";
import type { AppState } from "./app-state";
import {
  emptySnapshot,
  snapshotFromEntry,
  type PaletteConfig,
  type PaletteContext,
  type PaletteEntry,
  type PaletteKeyBinding,
  type PaletteOption,
  type PaletteSnapshot,
} from "./palette";

let nextId = 0;

export function createPaletteManager(setState: SetStoreFunction<AppState>) {
  const stack: PaletteEntry[] = [];

  function sync() {
    const top = stack[stack.length - 1];
    setState("palette", top ? snapshotFromEntry(top) : emptySnapshot);
  }

  function ctxFor(id: number): PaletteContext {
    return {
      dismiss() {
        const idx = stack.findIndex((e) => e.id === id);
        if (idx >= 0) {
          stack.splice(idx, 1);
          sync();
        }
      },
    };
  }

  function top(): PaletteEntry | undefined {
    return stack[stack.length - 1];
  }

  // ── Public API ──────────────────────────────────────────────────

  function show(
    config: PaletteConfig,
    keyBindings?: Record<string, PaletteKeyBinding>,
  ) {
    const id = nextId++;
    let entry: PaletteEntry;

    if ("mode" in config && config.mode === "input") {
      entry = {
        id,
        mode: "input",
        label: config.label ?? "",
        inputValue: config.inputValue ?? "",
        onSubmit: config.onSubmit,
      };
    } else {
      const opts = config as Exclude<PaletteConfig, { mode: "input" }>;
      entry = {
        id,
        mode: "list",
        options: opts.options,
        allOptions: opts.options,
        selectedIndex: 0,
        filterable: opts.filterable ?? false,
        filterText: "",
        hint: opts.hint ?? "",
        keyBindings: keyBindings ?? {},
      };
    }

    stack.push(entry);
    sync();
  }

  function updateTopOptions(options: PaletteOption[]) {
    const t = top();
    if (!t || t.mode !== "list") return;
    t.options = options;
    t.allOptions = options;
    t.selectedIndex = 0;
    t.filterText = "";
    sync();
  }

  function pop() {
    if (stack.length > 0) {
      stack.pop();
      sync();
    }
  }

  function clear() {
    stack.length = 0;
    sync();
  }

  // ── Key handling ────────────────────────────────────────────────

  function moveUp() {
    const t = top();
    if (!t || t.mode !== "list") return;
    const count = t.options.length;
    if (count === 0) return;
    t.selectedIndex = t.selectedIndex <= 0 ? count - 1 : t.selectedIndex - 1;
    sync();
  }

  function moveDown() {
    const t = top();
    if (!t || t.mode !== "list") return;
    const count = t.options.length;
    if (count === 0) return;
    t.selectedIndex = t.selectedIndex >= count - 1 ? 0 : t.selectedIndex + 1;
    sync();
  }

  function selectCurrent() {
    const t = top();
    if (!t || t.mode !== "list") return;
    const option = t.options[t.selectedIndex];
    if (option) {
      option.action(ctxFor(t.id));
    }
  }

  function filter(query: string) {
    const t = top();
    if (!t || t.mode !== "list" || !t.filterable) return;
    t.filterText = query;
    if (!query) {
      t.options = t.allOptions;
    } else {
      const q = query.toLowerCase();
      t.options = t.allOptions.filter(
        (o) =>
          o.name.toLowerCase().includes(q) ||
          o.description.toLowerCase().includes(q),
      );
    }
    t.selectedIndex = 0;
    sync();
  }

  function handleKeyBinding(key: string): boolean {
    const t = top();
    if (!t || t.mode !== "list") return false;
    const handler = t.keyBindings[key];
    if (!handler) return false;
    const option = t.options[t.selectedIndex];
    if (option) {
      handler(option, ctxFor(t.id));
    }
    return true;
  }

  function submitInput() {
    const t = top();
    if (!t || t.mode !== "input") return;
    t.onSubmit(t.inputValue, ctxFor(t.id));
  }

  function setInputValue(value: string) {
    const t = top();
    if (!t || t.mode !== "input") return;
    t.inputValue = value;
    sync();
  }

  return {
    show,
    updateTopOptions,
    pop,
    clear,
    moveUp,
    moveDown,
    selectCurrent,
    filter,
    handleKeyBinding,
    submitInput,
    setInputValue,
    get visible() {
      return stack.length > 0;
    },
    get isFilterable() {
      const t = top();
      return t?.mode === "list" && t.filterable;
    },
    get isInputMode() {
      const t = top();
      return t?.mode === "input";
    },
    get inputValue() {
      const t = top();
      return t?.mode === "input" ? t.inputValue : "";
    },
  };
}

export type PaletteManager = ReturnType<typeof createPaletteManager>;
