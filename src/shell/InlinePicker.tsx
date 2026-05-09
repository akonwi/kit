import type { KeyEvent } from "@opentui/core";
import { createMemo, For, Show } from "solid-js";
import type { PickerManager } from "../state/picker-manager";
import { computeScrollbar } from "./scrollbar";
import { theme } from "./theme";

const MAX_VISIBLE = 10;

export type InlinePickerProps = {
	picker: PickerManager;
	bottomOffset: number;
};

export function InlinePicker(props: InlinePickerProps) {
	const snapshot = () => props.picker.current();

	const visibleSlice = createMemo(() => {
		const p = snapshot();
		const options = p.options;
		const count = options.length;
		const selected = p.selectedIndex;

		if (count <= MAX_VISIBLE) {
			return {
				items: options.map((o, i) => ({ option: o, index: i })),
				offset: 0,
			};
		}

		let offset = selected - Math.floor(MAX_VISIBLE / 2);
		offset = Math.max(0, Math.min(offset, count - MAX_VISIBLE));

		const items = options
			.slice(offset, offset + MAX_VISIBLE)
			.map((o, i) => ({ option: o, index: offset + i }));

		return { items, offset };
	});

	const scrollbar = createMemo(() =>
		computeScrollbar(
			snapshot().options.length,
			MAX_VISIBLE,
			visibleSlice().offset,
		),
	);

	return (
		<Show when={snapshot().visible}>
			<box
				position="absolute"
				bottom={props.bottomOffset}
				left={0}
				width="100%"
				zIndex={100}
				border
				borderColor={theme.pickerBorder}
				backgroundColor={theme.pickerBg}
				paddingX={1}
				flexDirection="column"
			>
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
							onInput={(value: string) => props.picker.setInputValue(value)}
							onKeyDown={(e: KeyEvent) => {
								if (e.name === "return") {
									e.preventDefault();
									props.picker.submitInput();
								} else if (e.name === "escape") {
									e.preventDefault();
									props.picker.pop();
								}
							}}
						/>
					</box>
				</Show>

				<Show when={snapshot().mode === "list"}>
					<Show when={snapshot().filterable}>
						<box flexDirection="row" gap={1} width="100%">
							<text flexBasis={1} fg={theme.textPrimary}>
								{">"}
							</text>
							<input
								flexGrow={1}
								focused
								value={snapshot().filterText}
								onInput={(value: string) => props.picker.filter(value)}
								onKeyDown={(e: KeyEvent) => {
									if (e.name === "up") {
										e.preventDefault();
										props.picker.moveUp();
									} else if (e.name === "down") {
										e.preventDefault();
										props.picker.moveDown();
									} else if (e.name === "tab") {
										if (props.picker.handleKeyBinding("tab")) {
											e.preventDefault();
										}
									} else if (e.name === "return") {
										e.preventDefault();
										props.picker.selectCurrent();
									} else if (e.name === "escape") {
										e.preventDefault();
										props.picker.pop();
									} else if (e.ctrl && e.name) {
										const key = `ctrl+${e.name}`;
										if (props.picker.handleKeyBinding(key)) {
											e.preventDefault();
										}
									}
								}}
							/>
						</box>
					</Show>

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
										isFocused()
											? theme.pickerFocusedText
											: theme.pickerItemText;
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
												{isThumb ? "█" : "│"}
											</text>
										)}
									</For>
								</box>
							)}
						</Show>
					</box>

					<Show when={snapshot().hint && snapshot().hint !== "__commands__"}>
						<text fg={theme.textMuted}>{snapshot().hint}</text>
					</Show>
				</Show>
			</box>
		</Show>
	);
}
