import type { KeyEvent } from "@opentui/core";
import type { JSX } from "solid-js";
import { createContext, createMemo, For, Show, useContext } from "solid-js";
import type { PickerSnapshot } from "../state/picker";
import type { PickerManager } from "../state/picker-manager";
import { FULL_BLOCK, VERTICAL_LINE } from "./glyphs";

import { computeScrollbar } from "./scrollbar";
import { theme } from "./theme";

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
	if (e.name === "up") {
		e.preventDefault();
		picker.moveUp();
	} else if (e.name === "down") {
		e.preventDefault();
		picker.moveDown();
	} else if (e.name === "tab") {
		if (picker.handleKeyBinding("tab")) {
			e.preventDefault();
		}
	} else if (e.name === "return") {
		e.preventDefault();
		picker.selectCurrent();
	} else if (e.name === "escape") {
		e.preventDefault();
		picker.pop();
	} else if (e.ctrl && e.name) {
		const key = `ctrl+${e.name}`;
		if (picker.handleKeyBinding(key)) {
			e.preventDefault();
		}
	}
}

// ── Root ─────────────────────────────────────────────────────────────

export type RootProps = {
	picker: PickerManager;
	children: JSX.Element;
	maxVisible?: number;
};

function Root(props: RootProps) {
	const maxVisible = props.maxVisible ?? 5;
	const snapshot = () => props.picker.current();

	return (
		<PickerContext.Provider
			value={{ picker: props.picker, snapshot, maxVisible }}
		>
			{props.children}
		</PickerContext.Provider>
	);
}

// ── Header ──────────────────────────────────────────────────────────

function Header() {
	const { picker, snapshot } = usePickerContext();

	return (
		<box flexDirection="column" paddingY={1}>
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
						value={snapshot().inputValue}
						onInput={(value: string) => picker.setInputValue(value)}
						onKeyDown={(e: KeyEvent) => {
							if (e.name === "return") {
								e.preventDefault();
								picker.submitInput();
							} else if (e.name === "escape") {
								e.preventDefault();
								picker.pop();
							}
						}}
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
										<box
											flexShrink={0}
											flexDirection="row"
											height={1}
											overflow="hidden"
										>
											<text fg={fg()} bg={bg()}>
												{entry.option.name}
											</text>
											<Show when={entry.option.argHint}>
												<box flexShrink={1} height={1} overflow="hidden">
													<text fg={theme.textMuted} bg={bg()}>
														{` [${entry.option.argHint}]`}
													</text>
												</box>
											</Show>
										</box>
										<Show when={entry.option.description.length > 0}>
											<box flexGrow={1} flexShrink={1} />
											<box flexShrink={1} height={1} overflow="hidden">
												<text fg={fg()} bg={bg()}>
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

export type FooterProps = {
	children: JSX.Element;
};

function Footer(props: FooterProps) {
	return <box flexShrink={0}>{props.children}</box>;
}

// ── Export ───────────────────────────────────────────────────────────

export const Picker = {
	Root,
	Header,
	Body,
	Footer,
};
