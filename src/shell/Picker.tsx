import type { KeyEvent, Renderable } from "@opentui/core";
import { useBindings, useKeymap } from "@opentui/keymap/solid";
import type { BoxProps } from "@opentui/solid";
import type { Accessor, JSX } from "solid-js";
import {
	createContext,
	createMemo,
	createSignal,
	For,
	Show,
	useContext,
} from "solid-js";
import {
	createConfiguredBindings,
	type KitBindingDefinition,
	withKitKeyAliases,
} from "../keymap/bindings";
import type { Settings } from "../settings";
import type { PickerSnapshot } from "../state/picker";
import type { PickerManager } from "../state/picker-manager";
import { FULL_BLOCK, VERTICAL_LINE } from "./glyphs";
import { computeScrollbar } from "./scrollbar";
import { theme } from "./theme";

const PICKER_LIST_BINDINGS = [
	{
		cmd: "picker.move-up",
		key: "up",
		desc: "Move picker selection up",
		group: "Picker",
	},
	{
		cmd: "picker.move-down",
		key: "down",
		desc: "Move picker selection down",
		group: "Picker",
	},
	{
		cmd: "picker.complete",
		key: "tab",
		desc: "Complete picker selection",
		group: "Picker",
	},
	{
		cmd: "picker.select",
		key: "return",
		desc: "Select current picker item",
		group: "Picker",
	},
	{
		cmd: "picker.close",
		key: "escape",
		desc: "Close picker",
		group: "Picker",
	},
] as const satisfies readonly KitBindingDefinition[];

const PICKER_INPUT_BINDINGS = [
	{
		cmd: "picker.submit-input",
		key: "return",
		desc: "Submit picker input",
		group: "Picker",
	},
	{
		cmd: "picker.cancel-input",
		key: "escape",
		desc: "Cancel picker input",
		group: "Picker",
	},
] as const satisfies readonly KitBindingDefinition[];

// ── Context ─────────────────────────────────────────────────────────

type PickerContextValue = {
	picker: PickerManager;
	snapshot: () => PickerSnapshot;
	maxVisible: number;
};

const PickerContext = createContext<PickerContextValue>();

function usePickerContext(): PickerContextValue {
	const ctx = useContext(PickerContext);
	if (!ctx)
		throw new Error("Picker components must be used inside Picker.Root");
	return ctx;
}

// ── Key handler ─────────────────────────────────────────────────────

function handleListKeyDown(picker: PickerManager, e: KeyEvent) {
	if (e.ctrl && e.name) {
		const key = `ctrl+${e.name}`;
		if (picker.handleKeyBinding(key)) {
			e.preventDefault();
		}
	}
}

// ── Root ─────────────────────────────────────────────────────────────

export type RootProps = {
	settings?: Accessor<Settings>;
	picker: PickerManager;
	children: JSX.Element;
	maxVisible?: number;
};

function Root(props: RootProps) {
	const maxVisible = props.maxVisible ?? 5;
	const snapshot = () => props.picker.current();
	const keymap = useKeymap();
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);
	const userKeybindings = () => props.settings?.().keybindings;

	useBindings(() =>
		withKitKeyAliases({
			target: rootTarget,
			targetMode: "focus-within",
			enabled: () => snapshot().visible && snapshot().mode === "list",
			priority: 70,
			commands: [
				{
					name: "picker.move-up",
					desc: "Move picker selection up",
					group: "Picker",
					run: () => props.picker.moveUp(),
				},
				{
					name: "picker.move-down",
					desc: "Move picker selection down",
					group: "Picker",
					run: () => props.picker.moveDown(),
				},
				{
					name: "picker.complete",
					desc: "Complete picker selection",
					group: "Picker",
					run: () => props.picker.handleKeyBinding("tab"),
				},
				{
					name: "picker.select",
					desc: "Select current picker item",
					group: "Picker",
					run: () => props.picker.selectCurrent(),
				},
				{
					name: "picker.close",
					desc: "Close picker",
					group: "Picker",
					run: () => props.picker.pop(),
				},
			],
			bindings: createConfiguredBindings(
				keymap,
				PICKER_LIST_BINDINGS,
				userKeybindings(),
			),
		}),
	);

	useBindings(() =>
		withKitKeyAliases({
			target: rootTarget,
			targetMode: "focus-within",
			enabled: () => snapshot().visible && snapshot().mode === "input",
			priority: 70,
			commands: [
				{
					name: "picker.submit-input",
					desc: "Submit picker input",
					group: "Picker",
					run: () => props.picker.submitInput(),
				},
				{
					name: "picker.cancel-input",
					desc: "Cancel picker input",
					group: "Picker",
					run: () => props.picker.pop(),
				},
			],
			bindings: createConfiguredBindings(
				keymap,
				PICKER_INPUT_BINDINGS,
				userKeybindings(),
			),
		}),
	);

	return (
		<PickerContext.Provider
			value={{ picker: props.picker, snapshot, maxVisible }}
		>
			<box
				flexGrow={1}
				height="100%"
				flexDirection="column"
				gap={1}
				ref={(value) => setRootTarget(value as Renderable)}
			>
				{props.children}
			</box>
		</PickerContext.Provider>
	);
}

// ── Header ──────────────────────────────────────────────────────────

function Header() {
	const { picker, snapshot } = usePickerContext();

	return (
		<box flexDirection="column" paddingTop={1}>
			{/* Input mode */}
			<Show when={snapshot().mode === "input"}>
				<text fg={theme.textMuted}>{snapshot().label}</text>
				<box flexDirection="row" gap={1} width="100%">
					<text flexBasis={1} fg={theme.textPrimary}>
						{">"}
					</text>
					<input
						flexGrow={1}
						focused
						textColor={theme.textPrimary}
						focusedTextColor={theme.textPrimary}
						cursorColor={theme.cursor}
						value={snapshot().inputValue}
						onInput={(value: string) => picker.setInputValue(value)}
					/>
				</box>
			</Show>

			{/* List mode — filter input or focusable anchor for non-filterable */}
			<Show when={snapshot().mode === "list"}>
				<Show
					when={snapshot().filterable}
					fallback={
						<box
							focusable
							focused
							onKeyDown={(e: KeyEvent) => handleListKeyDown(picker, e)}
						/>
					}
				>
					<box flexDirection="row" gap={1} width="100%">
						<text flexBasis={1} fg={theme.textPrimary}>
							{">"}
						</text>
						<input
							flexGrow={1}
							focused
							textColor={theme.textPrimary}
							focusedTextColor={theme.textPrimary}
							cursorColor={theme.cursor}
							value={snapshot().filterText}
							onInput={(value: string) => picker.filter(value)}
							onKeyDown={(e: KeyEvent) => handleListKeyDown(picker, e)}
						/>
					</box>
				</Show>
			</Show>
		</box>
	);
}

// ── Body ────────────────────────────────────────────────────────────

function Body() {
	const { snapshot, maxVisible } = usePickerContext();

	const visibleSlice = createMemo(() => {
		const p = snapshot();
		const options = p.options;
		const count = options.length;
		const selected = p.selectedIndex;

		if (count <= maxVisible) {
			return {
				items: options.map((o, i) => ({ option: o, index: i })),
				offset: 0,
			};
		}

		let offset = selected - Math.floor(maxVisible / 2);
		offset = Math.max(0, Math.min(offset, count - maxVisible));

		const items = options
			.slice(offset, offset + maxVisible)
			.map((o, i) => ({ option: o, index: offset + i }));

		return { items, offset };
	});

	const scrollbar = createMemo(() =>
		computeScrollbar(
			snapshot().options.length,
			maxVisible,
			visibleSlice().offset,
		),
	);

	const widthOptions = createMemo(() => [
		...snapshot().allOptions,
		...snapshot().options,
	]);

	const maxNameWidth = createMemo(() =>
		Math.max(0, ...widthOptions().map((o) => o.name.length)),
	);

	const maxArgHintWidth = createMemo(() =>
		Math.max(
			0,
			...widthOptions().map((o) => (o.argHint ? o.argHint.length + 2 : 0)),
		),
	);

	return (
		<Show when={snapshot().mode === "list"}>
			<box flexGrow={1} flexDirection="column" overflow="hidden">
				<Show when={snapshot().options.length === 0}>
					<text fg={theme.textMuted}>No results</text>
				</Show>

				<box flexDirection="row">
					<box flexGrow={1} flexDirection="column">
						<For each={visibleSlice().items}>
							{(entry) => {
								const isFocused = () =>
									entry.index === snapshot().selectedIndex;
								const fg = () =>
									isFocused() ? theme.pickerFocusedText : theme.pickerItemText;
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
									>
										<box width={maxNameWidth()} flexShrink={0}>
											<text fg={fg()} bg={bg()}>
												{entry.option.name}
											</text>
										</box>
										<box width={maxArgHintWidth()} flexShrink={0}>
											<Show when={entry.option.argHint}>
												<text fg={theme.textMuted} bg={bg()}>
													{`[${entry.option.argHint}]`}
												</text>
											</Show>
										</box>
										<Show when={entry.option.description.length > 0}>
											<box
												flexGrow={1}
												flexShrink={1}
												height={1}
												overflow="hidden"
											>
												<text fg={theme.textMuted} bg={bg()}>
													{entry.option.description}
												</text>
											</box>
										</Show>
									</box>
								);
							}}
						</For>
					</box>
					<Show when={scrollbar()}>
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
			</box>
		</Show>
	);
}

// ── Footer ──────────────────────────────────────────────────────────

function Footer(props: BoxProps) {
	return (
		<box {...props} flexShrink={0}>
			{props.children}
		</box>
	);
}

// ── Export ───────────────────────────────────────────────────────────

export const Picker = {
	Root,
	Header,
	Body,
	Footer,
};
