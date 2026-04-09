import { createMemo, createSignal } from "solid-js";
import { scoreMatch } from "../features/files/score";
import {
	emptySnapshot,
	type PaletteConfig,
	type PaletteContext,
	type PaletteEntry,
	type PaletteKeyBinding,
	type PaletteOption,
	snapshotFromEntry,
} from "./palette";

let nextId = 0;

export function createPaletteManager() {
	const [stack, setStack] = createSignal<PaletteEntry[]>([]);

	const current = createMemo(() => {
		const s = stack();
		const t = s[s.length - 1];
		return t ? snapshotFromEntry(t) : emptySnapshot;
	});

	function top(): PaletteEntry | undefined {
		return stack().at(-1);
	}

	function updateTop(fn: (entry: PaletteEntry) => PaletteEntry) {
		setStack((s) => {
			if (s.length === 0) return s;
			const updated = fn(s[s.length - 1]);
			return [...s.slice(0, -1), updated];
		});
	}

	function ctxFor(id: number): PaletteContext {
		return {
			dismiss() {
				console.log("[palette] ctx.dismiss() called for id:", id);
				setStack((s) => {
					const entry = s.find((e) => e.id === id);
					console.log(
						"[palette] found entry:",
						!!entry,
						"calling onDismiss:",
						!!entry?.onDismiss,
					);
					entry?.onDismiss?.();
					return s.filter((e) => e.id !== id);
				});
			},
		};
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
				onDismiss: config.onDismiss,
				mode: "input",
				label: config.label ?? "",
				inputValue: config.inputValue ?? "",
				onSubmit: config.onSubmit,
			};
		} else if ("mode" in config && config.mode === "modal") {
			entry = {
				id,
				onDismiss: config.onDismiss,
				mode: "modal",
				title: config.title,
				lines: config.lines,
			};
		} else {
			const opts = config as Exclude<
				PaletteConfig,
				{ mode: "input" | "modal" }
			>;
			entry = {
				id,
				onDismiss: opts.onDismiss,
				onFilterChange: opts.onFilterChange,
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

		setStack((s) => [...s, entry]);
	}

	function pop() {
		setStack((s) => {
			const removed = s[s.length - 1];
			removed?.onDismiss?.();
			return s.slice(0, -1);
		});
	}

	function clear() {
		setStack([]);
	}

	// ── Key handling ────────────────────────────────────────────────

	function moveUp() {
		updateTop((t) => {
			if (t.mode !== "list") return t;
			const count = t.options.length;
			if (count === 0) return t;
			return {
				...t,
				selectedIndex: t.selectedIndex <= 0 ? count - 1 : t.selectedIndex - 1,
			};
		});
	}

	function moveDown() {
		updateTop((t) => {
			if (t.mode !== "list") return t;
			const count = t.options.length;
			if (count === 0) return t;
			return {
				...t,
				selectedIndex: t.selectedIndex >= count - 1 ? 0 : t.selectedIndex + 1,
			};
		});
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
		let override:
			| {
					query?: string;
					options?: PaletteOption[];
					selectedIndex?: number;
			  }
			| undefined;
		if (t?.onFilterChange) {
			const result = t.onFilterChange(query);
			if (result === false) return;
			if (result && typeof result === "object") {
				override = result;
			}
		}
		updateTop((t) => {
			if (t.mode !== "list" || !t.filterable) return t;
			const effectiveQuery = override?.query ?? query;
			const options = override?.options
				? override.options
				: effectiveQuery
					? t.allOptions
							.map((o) => {
								const nameScore = scoreMatch(o.name, effectiveQuery);
								const descScore = scoreMatch(o.description, effectiveQuery);
								return { option: o, score: Math.max(nameScore, descScore) };
							})
							.filter((e) => e.score > 0)
							.sort((a, b) => b.score - a.score)
							.map((e) => e.option)
					: t.allOptions;
			return {
				...t,
				filterText: query,
				options,
				selectedIndex: override?.selectedIndex ?? 0,
			};
		});
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
		updateTop((t) => {
			if (t.mode !== "input") return t;
			return { ...t, inputValue: value };
		});
	}

	const self = {
		current,
		show,
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
			return current().visible;
		},
		get isFilterable() {
			const c = current();
			return c.mode === "list" && c.filterable;
		},
		get isInputMode() {
			return current().mode === "input";
		},
		get inputValue() {
			const c = current();
			return c.mode === "input" ? c.inputValue : "";
		},
	};

	return self;
}

export type PaletteManager = ReturnType<typeof createPaletteManager>;
