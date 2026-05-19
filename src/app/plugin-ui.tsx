import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	type JSX,
	Show,
} from "solid-js";
import type { InternalPluginUI, TranscriptViewport } from "../plugins/types";
import { Dialog } from "../shell/Dialog";
import { CHEVRON_RIGHT } from "../shell/glyphs";
import { type Binding, HintBar } from "../shell/HintBar";
import { theme } from "../shell/theme";
import type { ToastInput } from "../state/toasts";
import type { OverlayComponentProps } from "./overlay-ui";

const SELECT_MAX_VISIBLE = 8;

const SELECT_BINDINGS: Binding[] = [
	{ key: "↑/↓", action: "move" },
	{ key: "Enter", action: "select" },
	{ key: "Esc", action: "cancel" },
];

const INPUT_BINDINGS: Binding[] = [
	{ key: "Enter", action: "submit" },
	{ key: "Esc", action: "cancel" },
];

const CONFIRM_BINDINGS: Binding[] = [
	{ key: "←/→", action: "choose" },
	{ key: "Enter", action: "confirm" },
	{ key: "Esc", action: "cancel" },
];

type OpenOverlay = <T>(
	component: (props: OverlayComponentProps<T>) => JSX.Element,
) => Promise<T>;

type SelectStringInput = {
	title: string;
	message?: string;
	options: string[];
	filterable?: boolean;
	placeholder?: string;
};

type SelectValueInput<T> = {
	title: string;
	message?: string;
	options: Array<{ label: string; value: T; description?: string }>;
	filterable?: boolean;
	placeholder?: string;
};

type SelectInput<T> = SelectStringInput | SelectValueInput<T>;

type NormalizedSelectOption = {
	label: string;
	description: string;
	value: unknown;
};

type InputOptions = {
	title: string;
	message?: string;
	placeholder?: string;
	initialValue?: string;
};

type ConfirmOptions = {
	title: string;
	message?: string;
	confirmLabel?: string;
	cancelLabel?: string;
	defaultValue?: boolean;
};

export type CreatePluginUIOptions = {
	toast: (toast: ToastInput) => void;
	custom: OpenOverlay;
	getTranscriptViewport: () => TranscriptViewport | null;
};

export function createPluginUI(
	options: CreatePluginUIOptions,
): InternalPluginUI {
	const select = ((input: SelectInput<unknown>) =>
		options.custom<unknown | undefined>((props) => (
			<PluginSelectOverlay {...props} input={input} />
		))) as InternalPluginUI["select"];

	return {
		text: (text, style) => ({ __kitText: true, text, style }),
		toast: options.toast,
		select,
		input: (input) =>
			options.custom<string | undefined>((props) => (
				<PluginInputOverlay {...props} input={input} />
			)),
		confirm: (input) =>
			options.custom<boolean>((props) => (
				<PluginConfirmOverlay {...props} input={input} />
			)),
		custom: options.custom,
		getTranscriptViewport: options.getTranscriptViewport,
	};
}

function normalizeSelectOptions(
	input: SelectInput<unknown>,
): NormalizedSelectOption[] {
	return input.options.map((option) => {
		if (typeof option === "string") {
			return { label: option, description: "", value: option };
		}
		return {
			label: option.label,
			description: option.description ?? "",
			value: option.value,
		};
	});
}

function matchesOption(option: NormalizedSelectOption, query: string): boolean {
	const needle = query.trim().toLowerCase();
	if (!needle) return true;
	return `${option.label} ${option.description}`.toLowerCase().includes(needle);
}

function PluginSelectOverlay(
	props: OverlayComponentProps<unknown | undefined> & {
		input: SelectInput<unknown>;
	},
) {
	const options = normalizeSelectOptions(props.input);
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [filter, setFilter] = createSignal("");
	const filteredOptions = createMemo(() =>
		props.input.filterable
			? options.filter((option) => matchesOption(option, filter()))
			: options,
	);

	createEffect(() => {
		const count = filteredOptions().length;
		if (selectedIndex() >= count) setSelectedIndex(Math.max(0, count - 1));
	});

	const visibleOptions = createMemo(() => {
		const all = filteredOptions();
		const selected = selectedIndex();
		if (all.length <= SELECT_MAX_VISIBLE) {
			return all.map((option, index) => ({ option, index }));
		}
		let offset = selected - Math.floor(SELECT_MAX_VISIBLE / 2);
		offset = Math.max(0, Math.min(offset, all.length - SELECT_MAX_VISIBLE));
		return all
			.slice(offset, offset + SELECT_MAX_VISIBLE)
			.map((option, index) => ({ option, index: offset + index }));
	});

	function move(delta: number) {
		const count = filteredOptions().length;
		if (count === 0) return;
		setSelectedIndex((current) => (current + delta + count) % count);
	}

	function submit() {
		props.done(filteredOptions()[selectedIndex()]?.value);
	}

	function handleKey(e: KeyEvent) {
		if (!props.active) return;
		if (e.name === "escape") {
			e.preventDefault();
			props.done(undefined);
			return;
		}
		if (e.name === "return") {
			e.preventDefault();
			submit();
			return;
		}
		if (e.name === "up") {
			e.preventDefault();
			move(-1);
			return;
		}
		if (e.name === "down") {
			e.preventDefault();
			move(1);
		}
	}

	useKeyboard(handleKey);

	return (
		<Dialog.Root
			surfaceProps={props.surfaceProps}
			width="70%"
			maxWidth={80}
			minWidth={40}
		>
			<Dialog.Header>
				<Dialog.Title>{props.input.title}</Dialog.Title>
			</Dialog.Header>
			<Show when={props.input.message}>
				<text fg={theme.textMuted}>{props.input.message}</text>
			</Show>
			<Show when={props.input.filterable}>
				<box flexDirection="row" gap={1} width="100%">
					<text flexBasis={1} fg={theme.textPrimary}>
						{">"}
					</text>
					<input
						flexGrow={1}
						focused={props.active}
						value={filter()}
						placeholder={props.input.placeholder ?? "Filter..."}
						placeholderColor={theme.textPlaceholder}
						onInput={(value: string) => setFilter(value)}
					/>
				</box>
			</Show>
			<Dialog.Body>
				<box flexDirection="column" overflow="hidden">
					<Show when={filteredOptions().length === 0}>
						<text fg={theme.textMuted}>No options</text>
					</Show>
					<For each={visibleOptions()}>
						{(entry) => {
							const isFocused = () => entry.index === selectedIndex();
							const fg = () =>
								isFocused() ? theme.pickerFocusedText : theme.textPrimary;
							const bg = () =>
								isFocused() ? theme.pickerFocusedBg : theme.bgTransparent;
							return (
								<box
									flexDirection="row"
									width="100%"
									height={1}
									overflow="hidden"
									gap={1}
									backgroundColor={bg()}
									onMouseUp={() => {
										if (!props.active) return;
										props.done(entry.option.value);
									}}
								>
									<text fg={fg()} bg={bg()}>
										{isFocused() ? `${CHEVRON_RIGHT} ` : "  "}
										{entry.option.label}
									</text>
									<Show when={entry.option.description.length > 0}>
										<box flexGrow={1} />
										<text fg={fg()} bg={bg()}>
											{entry.option.description}
										</text>
									</Show>
								</box>
							);
						}}
					</For>
				</box>
			</Dialog.Body>
			<Dialog.Footer>
				<HintBar borderless bindings={SELECT_BINDINGS} />
			</Dialog.Footer>
		</Dialog.Root>
	);
}

function PluginInputOverlay(
	props: OverlayComponentProps<string | undefined> & { input: InputOptions },
) {
	const [value, setValue] = createSignal(props.input.initialValue ?? "");

	function submit() {
		props.done(value());
	}

	function handleKey(e: KeyEvent) {
		if (!props.active) return;
		if (e.name === "escape") {
			e.preventDefault();
			props.done(undefined);
			return;
		}
		if (e.name === "return") {
			e.preventDefault();
			submit();
		}
	}

	useKeyboard(handleKey);

	return (
		<Dialog.Root
			surfaceProps={props.surfaceProps}
			width="60%"
			maxWidth={72}
			minWidth={40}
		>
			<Dialog.Header>
				<Dialog.Title>{props.input.title}</Dialog.Title>
			</Dialog.Header>
			<Show when={props.input.message}>
				<text fg={theme.textMuted}>{props.input.message}</text>
			</Show>
			<box border borderColor={theme.borderAccent} paddingX={1} width="100%">
				<input
					flexGrow={1}
					focused={props.active}
					value={value()}
					placeholder={props.input.placeholder ?? ""}
					placeholderColor={theme.textPlaceholder}
					onInput={(next: string) => setValue(next)}
				/>
			</box>
			<Dialog.Footer>
				<HintBar borderless bindings={INPUT_BINDINGS} />
			</Dialog.Footer>
		</Dialog.Root>
	);
}

function PluginConfirmOverlay(
	props: OverlayComponentProps<boolean> & { input: ConfirmOptions },
) {
	const [selected, setSelected] = createSignal(
		props.input.defaultValue ? 1 : 0,
	);
	const cancelLabel = () => props.input.cancelLabel ?? "Cancel";
	const confirmLabel = () => props.input.confirmLabel ?? "Confirm";

	function submit() {
		props.done(selected() === 1);
	}

	function handleKey(e: KeyEvent) {
		if (!props.active) return;
		if (e.name === "escape") {
			e.preventDefault();
			props.done(false);
			return;
		}
		if (e.name === "return") {
			e.preventDefault();
			submit();
			return;
		}
		if (e.name === "left" || e.name === "right") {
			e.preventDefault();
			setSelected((current) => (current === 0 ? 1 : 0));
		}
	}

	useKeyboard(handleKey);

	function option(label: string, index: number) {
		const focused = () => selected() === index;
		return (
			<box
				backgroundColor={
					focused() ? theme.pickerFocusedBg : theme.bgTransparent
				}
				paddingX={1}
				onMouseUp={() => {
					if (!props.active) return;
					props.done(index === 1);
				}}
			>
				<text
					fg={focused() ? theme.pickerFocusedText : theme.textPrimary}
					bg={focused() ? theme.pickerFocusedBg : theme.bgTransparent}
				>
					{label}
				</text>
			</box>
		);
	}

	return (
		<Dialog.Root
			surfaceProps={props.surfaceProps}
			width="50%"
			maxWidth={64}
			minWidth={36}
		>
			<Dialog.Header>
				<Dialog.Title>{props.input.title}</Dialog.Title>
			</Dialog.Header>
			<Show when={props.input.message}>
				<text fg={theme.textMuted}>{props.input.message}</text>
			</Show>
			<box flexGrow={1} />
			<box flexDirection="row" justifyContent="flex-end" gap={1}>
				{option(cancelLabel(), 0)}
				{option(confirmLabel(), 1)}
			</box>
			<Dialog.Footer>
				<HintBar borderless bindings={CONFIRM_BINDINGS} />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
