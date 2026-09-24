/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	type JSX,
	Show,
	untrack,
} from "solid-js";
import { scoreMatch } from "../features/files/score";
import { CHECK } from "../shell/glyphs";
import { DialogFrame } from "./DialogFrame";
import { OverlayHintBar } from "./OverlayHintBar";

export type PickerDialogOption<T> = {
	id: string;
	name: string;
	description?: string;
	value: T;
};

export function PickerDialog<T>(props: {
	open: boolean;
	id: string;
	title: string;
	options: readonly PickerDialogOption<T>[];
	currentId?: string;
	filter: "fuzzy" | "none";
	layout: "detail" | "single";
	loading?: boolean;
	error?: string;
	onCancel: () => void;
	onSelect: (value: T) => void;
}): JSX.Element {
	let input: HTMLInputElement | undefined;
	let listbox: HTMLDivElement | undefined;
	let wasOpen = false;
	const [query, setQuery] = createSignal("");
	const [selectedIndex, setSelectedIndex] = createSignal(0);

	const filteredOptions = createMemo(() => {
		const options = [...props.options];
		const needle = query().trim();
		if (props.filter === "none" || !needle) return options;
		return options
			.map((option) => ({
				option,
				score: Math.max(
					scoreMatch(option.name, needle),
					scoreMatch(option.description ?? "", needle),
				),
			}))
			.filter((entry) => entry.score > 0)
			.sort((a, b) => b.score - a.score)
			.map((entry) => entry.option);
	});
	const selectedOption = createMemo(
		() => filteredOptions()[selectedIndex()] ?? null,
	);

	function currentOptionIndex(
		options: readonly PickerDialogOption<T>[] = props.options,
		currentId: string | undefined = props.currentId,
	): number {
		const index = options.findIndex((option) => option.id === currentId);
		return Math.max(0, index);
	}

	createEffect(() => {
		if (props.open && !wasOpen) {
			setQuery("");
			setSelectedIndex(currentOptionIndex());
		}
		wasOpen = props.open;
	});

	createEffect(() => {
		const options = props.options;
		const currentId = props.currentId;
		if (!props.open || options.length === 0) return;
		setSelectedIndex(currentOptionIndex(untrack(filteredOptions), currentId));
		scrollSelectedIntoView();
	});

	createEffect(() => {
		const count = filteredOptions().length;
		setSelectedIndex((index) =>
			count === 0 ? 0 : Math.max(0, Math.min(index, count - 1)),
		);
	});

	function scrollSelectedIntoView(): void {
		queueMicrotask(() =>
			document
				.getElementById(`${props.id}-option-${selectedIndex()}`)
				?.scrollIntoView({ block: "nearest" }),
		);
	}

	function moveSelection(delta: -1 | 1): void {
		const count = filteredOptions().length;
		if (count === 0) return;
		setSelectedIndex((index) => (index + delta + count) % count);
		scrollSelectedIntoView();
	}

	function selectCurrent(): void {
		const option = selectedOption();
		if (option) props.onSelect(option.value);
	}

	function handleKeyDown(event: KeyboardEvent): void {
		if (event.key === "ArrowDown") {
			event.preventDefault();
			moveSelection(1);
		} else if (event.key === "ArrowUp") {
			event.preventDefault();
			moveSelection(-1);
		} else if (event.key === "Home") {
			event.preventDefault();
			setSelectedIndex(0);
			scrollSelectedIntoView();
		} else if (event.key === "End") {
			event.preventDefault();
			setSelectedIndex(Math.max(0, filteredOptions().length - 1));
			scrollSelectedIntoView();
		} else if (event.key === "Enter") {
			event.preventDefault();
			selectCurrent();
		}
	}

	const titleId = () => `${props.id}-title`;
	const listboxId = () => `${props.id}-listbox`;

	return (
		<DialogFrame
			open={props.open}
			id={props.id}
			class="picker-dialog"
			labelledBy={titleId()}
			focusKey={`${props.currentId ?? ""}:${props.options.length}`}
			onAfterOpen={() => {
				if (props.filter === "fuzzy") input?.focus();
				else listbox?.focus();
			}}
			onCancel={props.onCancel}
		>
			<header>
				<h2 id={titleId()}>{props.title}</h2>
				<Show when={props.filter === "fuzzy"}>
					<label class="picker-input">
						<span data-visually-hidden>Filter {props.title}</span>
						<input
							ref={input}
							type="text"
							value={query()}
							placeholder={`Filter ${props.title.toLowerCase()}`}
							role="combobox"
							aria-autocomplete="list"
							aria-expanded={true}
							aria-controls={listboxId()}
							aria-activedescendant={
								selectedOption()
									? `${props.id}-option-${selectedIndex()}`
									: undefined
							}
							onInput={(event) => {
								setQuery(event.currentTarget.value);
								setSelectedIndex(0);
								scrollSelectedIntoView();
							}}
							onKeyDown={handleKeyDown}
						/>
					</label>
				</Show>
			</header>
			<div class="picker-dialog-body">
				<Show when={props.error}>
					{(message) => (
						<div class="picker-message is-error" role="alert">
							{message()}
						</div>
					)}
				</Show>
				<Show when={!props.error && filteredOptions().length === 0}>
					<div class="picker-message" role="status" aria-live="polite">
						{props.loading ? "Loading…" : "No results"}
					</div>
				</Show>
				<div
					ref={listbox}
					id={listboxId()}
					class="picker-list"
					data-layout={props.layout}
					role="listbox"
					aria-busy={props.loading === true}
					tabIndex={props.filter === "none" ? 0 : -1}
					aria-label={props.title}
					aria-activedescendant={
						selectedOption()
							? `${props.id}-option-${selectedIndex()}`
							: undefined
					}
					onKeyDown={handleKeyDown}
				>
					<For each={filteredOptions()}>
						{(option, index) => (
							<button
								id={`${props.id}-option-${index()}`}
								class="picker-option"
								type="button"
								data-variant="ghost"
								role="option"
								tabIndex={-1}
								aria-selected={index() === selectedIndex()}
								aria-current={
									option.id === props.currentId ? "true" : undefined
								}
								onClick={() => props.onSelect(option.value)}
							>
								<span class="picker-option-name">
									{option.name}
									<Show when={option.id === props.currentId}>
										{` ${CHECK}`}
									</Show>
								</span>
								<span class="picker-option-description">
									{option.description ?? ""}
								</span>
							</button>
						)}
					</For>
				</div>
			</div>
			<OverlayHintBar
				class="picker-dialog-footer"
				hints={["Up/Down navigate", "Enter select", "Esc close"]}
			/>
		</DialogFrame>
	);
}
