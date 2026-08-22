import type { KeyEvent } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { createSignal, For, onCleanup } from "solid-js";
import { theme } from "./theme";

const MENU_WIDTH = 11;
const MENU_HEIGHT = 2;

export type SelectionContextMenuProps = {
	x: number;
	y: number;
	containerWidth: number;
	containerHeight: number;
	zIndex?: number;
	onCopy: () => void;
	onQuote: () => void;
	onClose: () => void;
};

export function resolveContextMenuPosition(options: {
	x: number;
	y: number;
	containerWidth: number;
	containerHeight: number;
	menuWidth: number;
	menuHeight: number;
}): { left: number; top: number } {
	const maxLeft = Math.max(1, options.containerWidth - options.menuWidth - 1);
	const left = Math.max(1, Math.min(options.x, maxLeft));
	const maxTop = Math.max(1, options.containerHeight - options.menuHeight - 1);
	const preferredTop =
		options.y + options.menuHeight <= options.containerHeight - 1
			? options.y
			: options.y - options.menuHeight + 1;
	return { left, top: Math.max(1, Math.min(preferredTop, maxTop)) };
}

export function resolveSelectionMenuPosition(options: {
	x: number;
	y: number;
	containerWidth: number;
	containerHeight: number;
}): { left: number; top: number } {
	return resolveContextMenuPosition({
		...options,
		menuWidth: MENU_WIDTH,
		menuHeight: MENU_HEIGHT,
	});
}

export function SelectionContextMenu(props: SelectionContextMenuProps) {
	const renderer = useRenderer();
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [pressedIndex, setPressedIndex] = createSignal<number | null>(null);
	const actions = [
		{ label: "Copy", run: props.onCopy },
		{ label: "Quote", run: props.onQuote },
	] as const;
	const position = () =>
		resolveSelectionMenuPosition({
			x: props.x,
			y: props.y,
			containerWidth: props.containerWidth,
			containerHeight: props.containerHeight,
		});

	function activate(index: number): void {
		actions[index]?.run();
	}

	function handleKeyDown(event: KeyEvent): void {
		if (event.name === "up" || event.name === "down") {
			event.preventDefault();
			setSelectedIndex((current) => (current === 0 ? 1 : 0));
			return;
		}
		if (event.name === "return") {
			event.preventDefault();
			activate(selectedIndex());
			return;
		}
		if (event.name === "escape") {
			event.preventDefault();
			props.onClose();
			return;
		}
		if (event.name === "c") {
			event.preventDefault();
			props.onCopy();
			return;
		}
		if (event.name === "q") {
			event.preventDefault();
			props.onQuote();
		}
	}

	onCleanup(() => renderer.setMousePointer("default"));

	return (
		<box
			position="absolute"
			left={position().left}
			top={position().top}
			width={MENU_WIDTH}
			height={MENU_HEIGHT}
			zIndex={props.zIndex ?? 60}
			flexDirection="column"
			backgroundColor={theme.pickerBg}
			focusable
			focused
			onKeyDown={handleKeyDown}
			onMouseDown={(event) => {
				event.preventDefault();
				event.stopPropagation();
			}}
		>
			<For each={actions}>
				{(action, index) => {
					const selected = () => selectedIndex() === index();
					return (
						<box
							width="100%"
							height={1}
							paddingX={1}
							backgroundColor={
								selected() ? theme.pickerFocusedBg : theme.pickerBg
							}
							onMouseOver={() => {
								setSelectedIndex(index());
								renderer.setMousePointer("pointer");
							}}
							onMouseOut={() => {
								setPressedIndex(null);
								renderer.setMousePointer("default");
							}}
							onMouseDown={(event) => {
								if (event.button !== 0) return;
								event.preventDefault();
								event.stopPropagation();
								setPressedIndex(index());
							}}
							onMouseUp={(event) => {
								if (event.button !== 0) return;
								event.preventDefault();
								event.stopPropagation();
								const shouldActivate = pressedIndex() === index();
								setPressedIndex(null);
								if (shouldActivate) activate(index());
							}}
						>
							<text
								selectable={false}
								fg={selected() ? theme.pickerFocusedText : theme.pickerItemText}
								bg={selected() ? theme.pickerFocusedBg : theme.pickerBg}
							>
								{action.label}
							</text>
						</box>
					);
				}}
			</For>
		</box>
	);
}
