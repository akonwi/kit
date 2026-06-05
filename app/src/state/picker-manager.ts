import { createMemo, createSignal } from "solid-js";
import { scoreMatch } from "../features/files/score";
import {
	emptySnapshot,
	type PickerConfig,
	type PickerContext,
	type PickerEntry,
	type PickerKeyBinding,
	type PickerOption,
	snapshotFromEntry,
} from "./picker";

let nextId = 0;

function filterOptions(options: PickerOption[], query: string): PickerOption[] {
	if (!query) return options;
	return options
		.map((o) => {
			const nameScore = scoreMatch(o.name, query);
			const descScore = scoreMatch(o.description, query);
			return { option: o, score: Math.max(nameScore, descScore) };
		})
		.filter((e) => e.score > 0)
		.sort((a, b) => b.score - a.score)
		.map((e) => e.option);
}

export function createPickerManager() {
	const [stack, setStack] = createSignal<PickerEntry[]>([]);

	const current = createMemo(() => {
		const s = stack();
		const t = s[s.length - 1];
		return t ? snapshotFromEntry(t) : emptySnapshot;
	});

	function top(): PickerEntry | undefined {
		return stack().at(-1);
	}

	function updateTop(fn: (entry: PickerEntry) => PickerEntry) {
		setStack((s) => {
			if (s.length === 0) return s;
			const updated = fn(s[s.length - 1]);
			return [...s.slice(0, -1), updated];
		});
	}

	function ctxFor(id: number): PickerContext {
		return {
			dismiss() {
				console.log("[picker] ctx.dismiss() called for id:", id);
				setStack((s) => {
					const entry = s.find((e) => e.id === id);
					console.log(
						"[picker] found entry:",
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
		config: PickerConfig,
		keyBindings?: Record<string, PickerKeyBinding>,
	) {
		const id = nextId++;
		const options = config.options ?? [];
		const initialIndex = Math.max(
			0,
			Math.min(config.selectedIndex ?? 0, options.length - 1),
		);
		const entry: PickerEntry = {
			id,
			onDismiss: config.onDismiss,
			onFilterChange: config.onFilterChange,
			onSubmit: config.onSubmit,
			onSelectionChange: config.onSelectionChange,
			prevSelectedOption: undefined,
			label: config.label ?? "",
			options,
			allOptions: options,
			selectedIndex: initialIndex,
			filterable: config.filterable ?? Boolean(config.onSubmit),
			filterText: config.inputValue ?? "",
			keyBindings: keyBindings ?? {},
			loading: config.loading ?? false,
		};

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

	function updateOptions(options: PickerOption[]) {
		updateTop((t) => ({
			...t,
			allOptions: options,
			options: filterOptions(options, t.filterText),
			selectedIndex: 0,
		}));
	}

	function setLoading(loading: boolean) {
		updateTop((t) => ({ ...t, loading }));
	}

	// ── Key handling ────────────────────────────────────────────────

	function notifySelectionChange() {
		const t = top();
		if (!t?.onSelectionChange) return;
		const option = t.options[t.selectedIndex];
		if (!option || option === t.prevSelectedOption) return;
		t.prevSelectedOption = option;
		t.onSelectionChange(option, t.selectedIndex);
	}

	function moveUp() {
		updateTop((t) => {
			const count = t.options.length;
			if (count === 0) return t;
			return {
				...t,
				selectedIndex: t.selectedIndex <= 0 ? count - 1 : t.selectedIndex - 1,
			};
		});
		notifySelectionChange();
	}

	function moveDown() {
		updateTop((t) => {
			const count = t.options.length;
			if (count === 0) return t;
			return {
				...t,
				selectedIndex: t.selectedIndex >= count - 1 ? 0 : t.selectedIndex + 1,
			};
		});
		notifySelectionChange();
	}

	function selectCurrent() {
		const t = top();
		if (!t) return;
		const option = t.options[t.selectedIndex];
		if (option) {
			option.action(ctxFor(t.id));
		}
	}

	function accept() {
		const t = top();
		if (!t) return;
		if (t.onSubmit) {
			t.onSubmit(t.filterText, ctxFor(t.id));
			return;
		}
		selectCurrent();
	}

	function filter(query: string) {
		const t = top();
		let override:
			| {
					query?: string;
					options?: PickerOption[];
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
			if (!t.filterable) return t;
			const effectiveQuery = override?.query ?? query;
			const options = override?.options
				? override.options
				: filterOptions(t.allOptions, effectiveQuery);
			return {
				...t,
				filterText: query,
				options,
				selectedIndex: override?.selectedIndex ?? 0,
			};
		});
		notifySelectionChange();
	}

	function handleKeyBinding(key: string): boolean {
		const t = top();
		if (!t) return false;
		const handler = t.keyBindings[key];
		if (!handler) return false;
		const option = t.options[t.selectedIndex];
		if (option) {
			handler(option, ctxFor(t.id));
		}
		return true;
	}

	const self = {
		current,
		show,
		pop,
		clear,
		moveUp,
		moveDown,
		selectCurrent,
		accept,
		filter,
		updateOptions,
		setLoading,
		handleKeyBinding,
		get visible() {
			return current().visible;
		},
		get isFilterable() {
			return current().filterable;
		},
	};

	return self;
}

export type PickerManager = ReturnType<typeof createPickerManager>;
