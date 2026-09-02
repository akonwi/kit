import {
	createEffect,
	createMemo,
	createSignal,
	For,
	type JSX,
	Show,
} from "solid-js";
import { useKeymapLayer } from "../keymap/useKeymapLayer";
import type { InternalPluginUI, TranscriptViewport } from "../plugins/types";
import { FULL_BLOCK, VERTICAL_LINE } from "../shell/glyphs";
import { InteractionDock } from "../shell/InteractionDock";
import { KeymapHintBar } from "../shell/KeymapHintBar";
import { KitMarkdown } from "../shell/KitMarkdown";
import { computeScrollbar } from "../shell/scrollbar";
import { scrollbarStyle, theme } from "../shell/theme";
import type { ToastInput } from "../state/toasts";
import type {
	InteractionComponentProps,
	OpenInteraction,
} from "./interaction-ui";
import type { OverlayComponentProps } from "./overlay-ui";

const SELECT_MAX_VISIBLE = 8;
const CONFIRM_MAX_MESSAGE_ROWS = 12;
const CONFIRM_FIXED_ROWS = 8;

type OpenOverlay = <T>(
	component: (props: OverlayComponentProps<T>) => JSX.Element,
) => Promise<T>;

type SelectStringInput = {
	title: string;
	message?: string;
	options: string[];
	filterable?: boolean;
	placeholder?: string;
	signal?: AbortSignal;
};

type SelectValueInput<T> = {
	title: string;
	message?: string;
	options: Array<{ label: string; value: T; description?: string }>;
	filterable?: boolean;
	placeholder?: string;
	signal?: AbortSignal;
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
	signal?: AbortSignal;
};

type ConfirmOptions = {
	title: string;
	message?: string;
	confirmLabel?: string;
	cancelLabel?: string;
	defaultValue?: boolean;
	signal?: AbortSignal;
};

export function measurePluginConfirmMessageHeight(
	message: string,
	width: number,
	maxHeight: number,
): number {
	const contentWidth = Math.max(1, width - 1);
	const rows = message.split("\n").reduce((total, line) => {
		return total + Math.max(1, Math.ceil(Bun.stringWidth(line) / contentWidth));
	}, 0);
	return Math.max(
		1,
		Math.min(
			CONFIRM_MAX_MESSAGE_ROWS,
			Math.max(1, maxHeight - CONFIRM_FIXED_ROWS),
			rows,
		),
	);
}

export type CreatePluginUIOptions = {
	toast: (toast: ToastInput) => void;
	custom: OpenOverlay;
	interaction: OpenInteraction;
	getTranscriptViewport: () => TranscriptViewport | null;
	getTheme: InternalPluginUI["theme"];
};

export function createPluginUI(
	options: CreatePluginUIOptions,
): InternalPluginUI {
	const select = ((input: SelectInput<unknown>) =>
		options.interaction<unknown | undefined>(
			(props) => <PluginSelectInteraction {...props} input={input} />,
			{ signal: input.signal, abortValue: undefined },
		)) as InternalPluginUI["select"];

	return {
		text: (text, style) => ({ __kitText: true, text, style }),
		theme: options.getTheme,
		toast: options.toast,
		select,
		input: (input) =>
			options.interaction<string | undefined>(
				(props) => <PluginInputInteraction {...props} input={input} />,
				{ signal: input.signal, abortValue: undefined },
			),
		confirm: (input) =>
			options.interaction<boolean>(
				(props) => <PluginConfirmInteraction {...props} input={input} />,
				{ signal: input.signal, abortValue: false },
			),
		custom: options.custom,
		interaction: options.interaction,
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

export function PluginSelectInteraction(
	props: InteractionComponentProps<unknown | undefined> & {
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

	const maxVisibleOptions = () =>
		Math.max(
			1,
			Math.min(
				SELECT_MAX_VISIBLE,
				props.maxHeight -
					6 -
					(props.input.message ? 2 : 0) -
					(props.input.filterable ? 2 : 0),
			),
		);
	const visibleOptions = createMemo(() => {
		const all = filteredOptions();
		const selected = selectedIndex();
		const visibleCount = maxVisibleOptions();
		if (all.length <= visibleCount) {
			return {
				items: all.map((option, index) => ({ option, index })),
				offset: 0,
			};
		}
		let offset = selected - Math.floor(visibleCount / 2);
		offset = Math.max(0, Math.min(offset, all.length - visibleCount));
		return {
			items: all
				.slice(offset, offset + visibleCount)
				.map((option, index) => ({ option, index: offset + index })),
			offset,
		};
	});
	const scrollbar = createMemo(() =>
		computeScrollbar(
			filteredOptions().length,
			maxVisibleOptions(),
			visibleOptions().offset,
		),
	);

	function move(delta: number) {
		const count = filteredOptions().length;
		if (count === 0) return;
		setSelectedIndex((current) => (current + delta + count) % count);
	}

	function submit() {
		props.done(filteredOptions()[selectedIndex()]?.value);
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active,
		commands: {
			"plugin-ui.select.cancel": () => props.done(undefined),
			"plugin-ui.select.submit": submit,
			"plugin-ui.select.move-up": () => move(-1),
			"plugin-ui.select.move-down": () => move(1),
		},
	}));

	return (
		<InteractionDock.Root maxHeight={props.maxHeight}>
			<InteractionDock.Header
				meta={<InteractionDock.Meta>Choose one</InteractionDock.Meta>}
			>
				<InteractionDock.Title>{props.input.title}</InteractionDock.Title>
			</InteractionDock.Header>
			<Show when={props.input.message}>
				<text fg={theme.textSecondary} wrapMode="none">
					{props.input.message}
				</text>
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
			<InteractionDock.Body>
				<Show when={filteredOptions().length === 0} fallback={null}>
					<text fg={theme.textMuted}>No options</text>
				</Show>
				<box flexDirection="row" overflow="hidden">
					<box flexGrow={1} flexDirection="column" overflow="hidden">
						<For each={visibleOptions().items}>
							{(entry) => {
								const isFocused = () => entry.index === selectedIndex();
								const labelFg = () =>
									isFocused() ? theme.pickerFocusedText : theme.pickerItemText;
								const descriptionFg = () =>
									isFocused() ? theme.pickerFocusedText : theme.textMuted;
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
										<text fg={labelFg()} bg={bg()}>
											{entry.option.label}
										</text>
										<Show
											when={entry.option.description.length > 0}
											fallback={null}
										>
											<box flexGrow={1} />
											<text fg={descriptionFg()} bg={bg()}>
												{entry.option.description}
											</text>
										</Show>
									</box>
								);
							}}
						</For>
					</box>
					<Show when={scrollbar()} fallback={null}>
						{(track) => (
							<box flexShrink={0} width={1} flexDirection="column">
								<For each={track()}>
									{(isThumb) => (
										<text
											fg={
												isThumb
													? theme.pickerScrollThumb
													: theme.pickerScrollTrack
											}
										>
											{isThumb ? FULL_BLOCK : VERTICAL_LINE}
										</text>
									)}
								</For>
							</box>
						)}
					</Show>
				</box>
			</InteractionDock.Body>
			<InteractionDock.Footer>
				<KeymapHintBar borderless group="plugin-ui.select" />
			</InteractionDock.Footer>
		</InteractionDock.Root>
	);
}

export function PluginInputInteraction(
	props: InteractionComponentProps<string | undefined> & {
		input: InputOptions;
	},
) {
	const [value, setValue] = createSignal(props.input.initialValue ?? "");

	function submit() {
		props.done(value());
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active,
		commands: {
			"plugin-ui.input.cancel": () => props.done(undefined),
			"plugin-ui.input.submit": submit,
		},
	}));

	return (
		<InteractionDock.Root maxHeight={props.maxHeight}>
			<InteractionDock.Header
				meta={<InteractionDock.Meta>Short answer</InteractionDock.Meta>}
			>
				<InteractionDock.Title>{props.input.title}</InteractionDock.Title>
			</InteractionDock.Header>
			<InteractionDock.Body gap={1}>
				<Show when={props.input.message}>
					<text fg={theme.textSecondary} wrapMode="none">
						{props.input.message}
					</text>
				</Show>
				<box
					flexShrink={0}
					border
					borderColor={props.active ? theme.borderAccent : theme.borderDefault}
					paddingX={1}
					width="100%"
				>
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
			</InteractionDock.Body>
			<InteractionDock.Footer>
				<KeymapHintBar borderless group="plugin-ui.input" />
			</InteractionDock.Footer>
		</InteractionDock.Root>
	);
}

export function PluginConfirmInteraction(
	props: InteractionComponentProps<boolean> & { input: ConfirmOptions },
) {
	let messageScrollRef:
		| { scrollBy: (options: { x: number; y: number }) => void }
		| undefined;
	const [messageWidth, setMessageWidth] = createSignal(80);
	const [selected, setSelected] = createSignal(
		props.input.defaultValue ? 1 : 0,
	);
	const messageHeight = createMemo(() =>
		measurePluginConfirmMessageHeight(
			props.input.message ?? "",
			messageWidth(),
			props.maxHeight,
		),
	);
	const cancelLabel = () => props.input.cancelLabel ?? "Cancel";
	const confirmLabel = () => props.input.confirmLabel ?? "Confirm";

	function submit() {
		props.done(selected() === 1);
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active,
		commands: {
			"plugin-ui.confirm.cancel": () => props.done(false),
			"plugin-ui.confirm.submit": submit,
			"plugin-ui.confirm.choose-previous": () => {
				setSelected((current) => (current === 0 ? 1 : 0));
			},
			"plugin-ui.confirm.choose-next": () => {
				setSelected((current) => (current === 0 ? 1 : 0));
			},
			"plugin-ui.confirm.scroll-up": () =>
				messageScrollRef?.scrollBy({ x: 0, y: -3 }),
			"plugin-ui.confirm.scroll-down": () =>
				messageScrollRef?.scrollBy({ x: 0, y: 3 }),
		},
	}));

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
		<InteractionDock.Root maxHeight={props.maxHeight}>
			<InteractionDock.Header
				meta={<InteractionDock.Meta>Confirmation</InteractionDock.Meta>}
			>
				<InteractionDock.Title>{props.input.title}</InteractionDock.Title>
			</InteractionDock.Header>
			<Show when={props.input.message}>
				<InteractionDock.Body>
					<scrollbox
						ref={(value) => {
							messageScrollRef = value as typeof messageScrollRef;
						}}
						onSizeChange={() => {
							const width = (messageScrollRef as { width?: number } | undefined)
								?.width;
							if (width && width !== messageWidth()) setMessageWidth(width);
						}}
						height={messageHeight()}
						flexShrink={1}
						scrollY
						style={scrollbarStyle()}
					>
						<KitMarkdown
							content={props.input.message ?? ""}
							fg={theme.textSecondary}
						/>
					</scrollbox>
				</InteractionDock.Body>
			</Show>
			<box flexShrink={0} flexDirection="row" justifyContent="flex-end" gap={1}>
				{option(cancelLabel(), 0)}
				{option(confirmLabel(), 1)}
			</box>
			<InteractionDock.Footer>
				<KeymapHintBar borderless group="plugin-ui.confirm" />
			</InteractionDock.Footer>
		</InteractionDock.Root>
	);
}
