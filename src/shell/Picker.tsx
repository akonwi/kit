import type { KeyEvent, Renderable } from "@opentui/core";
import { useBindings, useKeymap } from "@opentui/keymap/solid";
import type { BoxProps } from "@opentui/solid";
import type { Accessor, JSX } from "solid-js";
import {
	createContext,
	createEffect,
	createMemo,
	createSignal,
	For,
	Show,
	useContext,
} from "solid-js";
import {
	type BindingDefinition,
	createConfiguredBindingResult,
	type KeybindingDiagnostic,
	withKitKeyAliases,
} from "../keymap/bindings";
import { reportKeybindingDiagnostics } from "../keymap/diagnostics";
import type { Settings } from "../settings";
import type { PickerSnapshot } from "../state/picker";
import type { PickerManager } from "../state/picker-manager";
import { FULL_BLOCK, VERTICAL_LINE } from "./glyphs";
import { computeScrollbar } from "./scrollbar";
import { theme } from "./theme";

type PickerCommandDescriptor = {
	id: string;
	key: string;
	desc: string;
	hint: string;
	run: (picker: PickerManager) => boolean | undefined;
};

function pickerDescriptors(
	includeComplete: boolean,
	selectHint: string,
): PickerCommandDescriptor[] {
	const descriptors: PickerCommandDescriptor[] = [
		{
			id: "move-up",
			key: "up",
			desc: "Move selection up",
			hint: "up",
			run: (picker) => {
				picker.moveUp();
				return undefined;
			},
		},
		{
			id: "move-down",
			key: "down",
			desc: "Move selection down",
			hint: "down",
			run: (picker) => {
				picker.moveDown();
				return undefined;
			},
		},
	];
	if (includeComplete) {
		descriptors.push({
			id: "complete",
			key: "tab",
			desc: "Complete selection",
			hint: "complete",
			run: (picker) => picker.handleKeyBinding("tab"),
		});
	}
	descriptors.push(
		{
			id: "select",
			key: "return",
			desc: "Select or submit picker value",
			hint: selectHint,
			run: (picker) => {
				picker.accept();
				return undefined;
			},
		},
		{
			id: "close",
			key: "escape",
			desc: "Close picker",
			hint: "close",
			run: (picker) => {
				picker.pop();
				return undefined;
			},
		},
	);
	return descriptors;
}

function pickerBindingDefinitions(
	namespace: string,
	descriptors: readonly PickerCommandDescriptor[],
): BindingDefinition[] {
	return descriptors.map((descriptor) => ({
		cmd: `${namespace}.${descriptor.id}`,
		key: descriptor.key,
		desc: descriptor.desc,
		group: namespace,
	}));
}

function pickerCommands(
	namespace: string,
	picker: PickerManager,
	descriptors: readonly PickerCommandDescriptor[],
) {
	return descriptors.map((descriptor) => ({
		name: `${namespace}.${descriptor.id}`,
		desc: descriptor.desc,
		group: namespace,
		hint: descriptor.hint,
		run: () => descriptor.run(picker),
	}));
}

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
	onKeybindingDiagnostic?: (diagnostic: KeybindingDiagnostic) => void;
	picker: PickerManager;
	children: JSX.Element;
	maxVisible?: number;
	commandNamespace?: string;
	includeCompleteBinding?: boolean;
	selectHint?: string;
};

function Root(props: RootProps) {
	const maxVisible = props.maxVisible ?? 5;
	const snapshot = () => props.picker.current();
	const keymap = useKeymap();
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);
	const commandNamespace = () => props.commandNamespace ?? "picker";
	const selectHint = () => props.selectHint ?? "select";
	const userKeybindings = () => props.settings?.().keybindings;
	const descriptors = createMemo(() =>
		pickerDescriptors(props.includeCompleteBinding === true, selectHint()),
	);
	const bindings = createMemo(() => {
		const namespace = commandNamespace();
		return createConfiguredBindingResult(
			keymap,
			pickerBindingDefinitions(namespace, descriptors()),
			userKeybindings(),
		);
	});

	createEffect(() => {
		reportKeybindingDiagnostics(
			bindings().diagnostics,
			props.onKeybindingDiagnostic,
		);
	});

	useBindings(() => {
		const namespace = commandNamespace();
		return withKitKeyAliases({
			target: rootTarget,
			targetMode: "focus-within",
			enabled: () => snapshot().visible,
			priority: 70,
			commands: pickerCommands(namespace, props.picker, descriptors()),
			bindings: bindings().bindings,
		});
	});

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
			<Show when={snapshot().label}>
				<text fg={theme.textMuted}>{snapshot().label}</text>
			</Show>
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
		<box flexGrow={1} flexDirection="column" overflow="hidden">
			<Show when={snapshot().options.length === 0}>
				<text fg={theme.textMuted}>No results</text>
			</Show>

			<box flexDirection="row">
				<box flexGrow={1} flexDirection="column">
					<For each={visibleSlice().items}>
						{(entry) => {
							const isFocused = () => entry.index === snapshot().selectedIndex;
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
