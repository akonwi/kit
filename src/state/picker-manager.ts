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
		const entry: PickerEntry = {
			id,
			onDismiss: config.onDismiss,
			onFilterChange: config.onFilterChange,
			onSubmit: config.onSubmit,
			label: config.label ?? "",
			options,
			allOptions: options,
			selectedIndex: 0,
			filterable: config.filterable ?? Boolean(config.onSubmit),
			filterText: config.inputValue ?? "",
			hint: config.hint ?? "",
			keyBindings: keyBindings ?? {},
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

	// ── Key handling ────────────────────────────────────────────────

	function moveUp() {
		updateTop((t) => {
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
