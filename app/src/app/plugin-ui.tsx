import { useBindings } from "@opentui/keymap/solid";
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	type JSX,
	Show,
} from "solid-js";
import { withKitKeyAliases } from "../keymap/bindings";
import type { InternalPluginUI, TranscriptViewport } from "../plugins/types";
import { Dialog } from "../shell/Dialog";
import { CHEVRON_RIGHT } from "../shell/glyphs";
import { KeymapHintBar } from "../shell/KeymapHintBar";
import { theme } from "../shell/theme";
import type { ToastInput } from "../state/toasts";
import type { OverlayComponentProps } from "./overlay-ui";

const SELECT_MAX_VISIBLE = 8;

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
	getTheme: InternalPluginUI["theme"];
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
		theme: options.getTheme,
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

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => props.active,
			priority: 200,
			commands: [
				{
					name: "plugin-ui.select.cancel",
					desc: "Cancel plugin selection",
					group: "plugin-ui.select",
					hint: "cancel",
					run: () => props.done(undefined),
				},
				{
					name: "plugin-ui.select.submit",
					desc: "Submit plugin selection",
					group: "plugin-ui.select",
					hint: "select",
					run: submit,
				},
				{
					name: "plugin-ui.select.move-up",
					desc: "Move plugin selection up",
					group: "plugin-ui.select",
					hint: "move",
					run: () => move(-1),
				},
				{
					name: "plugin-ui.select.move-down",
					desc: "Move plugin selection down",
					group: "plugin-ui.select",
					hint: "move",
					run: () => move(1),
				},
			],
			bindings: [
				{
					key: "escape",
					cmd: "plugin-ui.select.cancel",
					desc: "Cancel plugin selection",
					group: "plugin-ui.select",
				},
				{
					key: "return",
					cmd: "plugin-ui.select.submit",
					desc: "Submit plugin selection",
					group: "plugin-ui.select",
				},
				{
					key: "up",
					cmd: "plugin-ui.select.move-up",
					desc: "Move plugin selection up",
					group: "plugin-ui.select",
				},
				{
					key: "down",
					cmd: "plugin-ui.select.move-down",
					desc: "Move plugin selection down",
					group: "plugin-ui.select",
				},
			],
		}),
	);

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
						backgroundColor={theme.bgTransparent}
						focusedBackgroundColor={theme.bgTransparent}
						textColor={theme.textPrimary}
						focusedTextColor={theme.textPrimary}
						cursorColor={theme.cursor}
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
				<KeymapHintBar borderless group="plugin-ui.select" />
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

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => props.active,
			priority: 200,
			commands: [
				{
					name: "plugin-ui.input.cancel",
					desc: "Cancel plugin input",
					group: "plugin-ui.input",
					hint: "cancel",
					run: () => props.done(undefined),
				},
				{
					name: "plugin-ui.input.submit",
					desc: "Submit plugin input",
					group: "plugin-ui.input",
					hint: "submit",
					run: submit,
				},
			],
			bindings: [
				{
					key: "escape",
					cmd: "plugin-ui.input.cancel",
					desc: "Cancel plugin input",
					group: "plugin-ui.input",
				},
				{
					key: "return",
					cmd: "plugin-ui.input.submit",
					desc: "Submit plugin input",
					group: "plugin-ui.input",
				},
			],
		}),
	);

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
					backgroundColor={theme.bgTransparent}
					focusedBackgroundColor={theme.bgTransparent}
					textColor={theme.textPrimary}
					focusedTextColor={theme.textPrimary}
					cursorColor={theme.cursor}
					onInput={(next: string) => setValue(next)}
				/>
			</box>
			<Dialog.Footer>
				<KeymapHintBar borderless group="plugin-ui.input" />
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

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => props.active,
			priority: 200,
			commands: [
				{
					name: "plugin-ui.confirm.cancel",
					desc: "Cancel plugin confirmation",
					group: "plugin-ui.confirm",
					hint: "cancel",
					run: () => props.done(false),
				},
				{
					name: "plugin-ui.confirm.submit",
					desc: "Submit plugin confirmation",
					group: "plugin-ui.confirm",
					hint: "confirm",
					run: submit,
				},
				{
					name: "plugin-ui.confirm.choose-previous",
					desc: "Choose previous confirmation option",
					group: "plugin-ui.confirm",
					hint: "choose",
					run: () => {
						setSelected((current) => (current === 0 ? 1 : 0));
					},
				},
				{
					name: "plugin-ui.confirm.choose-next",
					desc: "Choose next confirmation option",
					group: "plugin-ui.confirm",
					hint: "choose",
					run: () => {
						setSelected((current) => (current === 0 ? 1 : 0));
					},
				},
			],
			bindings: [
				{
					key: "escape",
					cmd: "plugin-ui.confirm.cancel",
					desc: "Cancel plugin confirmation",
					group: "plugin-ui.confirm",
				},
				{
					key: "return",
					cmd: "plugin-ui.confirm.submit",
					desc: "Submit plugin confirmation",
					group: "plugin-ui.confirm",
				},
				{
					key: "left",
					cmd: "plugin-ui.confirm.choose-previous",
					desc: "Choose previous confirmation option",
					group: "plugin-ui.confirm",
				},
				{
					key: "right",
					cmd: "plugin-ui.confirm.choose-next",
					desc: "Choose next confirmation option",
					group: "plugin-ui.confirm",
				},
			],
		}),
	);

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
				<KeymapHintBar borderless group="plugin-ui.confirm" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
