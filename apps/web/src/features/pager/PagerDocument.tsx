import type { MouseEvent } from "@opentui/core";
import { useRenderer } from "@opentui/solid";
import { createSignal, onCleanup, Show } from "solid-js";
import { KitMarkdown } from "../../shell/KitMarkdown";
import { SelectionContextMenu } from "../../shell/SelectionContextMenu";
import {
	applySelectionColors,
	restoreSelectionColors,
	type SelectionColorRestore,
} from "../../shell/selection";
import { scrollbarStyle, theme } from "../../shell/theme";

export type PagerSelectionMenuController = {
	close: () => void;
};

export type PagerDocumentProps = {
	sectionTitle: string;
	body: string;
	zIndex: number;
	bindScroll: (
		ref: { scrollBy: (options: { x: number; y: number }) => void } | undefined,
	) => void;
	copyText: (text: string) => Promise<void>;
	onCopyError: (error: unknown) => void;
	onQuote: (text: string) => void;
	onMenuVisibilityChange?: (visible: boolean) => void;
	registerSelectionMenu?: (
		controller: PagerSelectionMenuController | undefined,
	) => void;
};

export function PagerDocument(props: PagerDocumentProps) {
	const renderer = useRenderer();
	let containerRef:
		| { x: number; y: number; width: number; height: number }
		| undefined;
	const [selectionMenu, setSelectionMenu] = createSignal<{
		text: string;
		x: number;
		y: number;
	} | null>(null);
	const selectionColorRestore: SelectionColorRestore = new Map();

	function colorCurrentSelection(): void {
		const selection = renderer.getSelection();
		if (!selection) return;
		applySelectionColors(
			selection,
			selectionColorRestore,
			theme.pickerFocusedBg,
			theme.pickerFocusedText,
		);
	}

	function discardSelection(): void {
		restoreSelectionColors(selectionColorRestore);
		renderer.clearSelection();
	}

	function closeSelectionMenu(): void {
		setSelectionMenu(null);
		props.onMenuVisibilityChange?.(false);
		discardSelection();
	}

	function handleMouseDown(event: MouseEvent): void {
		event.stopPropagation();
		if (selectionMenu()) closeSelectionMenu();
		colorCurrentSelection();
	}

	function handleMouseMove(event: MouseEvent): void {
		event.stopPropagation();
		colorCurrentSelection();
	}

	function handleMouseUp(event: MouseEvent): void {
		event.stopPropagation();
		if (event.button !== 0) return;
		colorCurrentSelection();
		const selection = renderer.getSelection();
		const text = selection?.getSelectedText();
		if (!selection || !text) {
			discardSelection();
			return;
		}
		setSelectionMenu({
			text,
			x: event.x - (containerRef?.x ?? 0),
			y: event.y - (containerRef?.y ?? 0),
		});
		props.onMenuVisibilityChange?.(true);
	}

	function copySelectedText(): void {
		const selected = selectionMenu();
		if (!selected) return;
		closeSelectionMenu();
		void props.copyText(selected.text).catch(props.onCopyError);
	}

	function quoteSelectedText(): void {
		const selected = selectionMenu();
		if (!selected) return;
		closeSelectionMenu();
		props.onQuote(selected.text);
	}

	props.registerSelectionMenu?.({ close: closeSelectionMenu });
	onCleanup(() => {
		props.registerSelectionMenu?.(undefined);
		props.onMenuVisibilityChange?.(false);
		discardSelection();
	});

	return (
		<box
			ref={(value) => {
				containerRef = value as typeof containerRef;
			}}
			flexGrow={1}
			flexDirection="column"
		>
			<scrollbox
				ref={props.bindScroll}
				onMouseDown={handleMouseDown}
				onMouseMove={handleMouseMove}
				onMouseDrag={handleMouseMove}
				onMouseUp={handleMouseUp}
				flexGrow={1}
				scrollY
				stickyStart="top"
				paddingX={1}
				paddingY={1}
				style={scrollbarStyle()}
			>
				<box flexDirection="column" width="100%">
					{props.sectionTitle.length > 0 ? (
						<text fg={theme.textMuted}>
							<b>{props.sectionTitle}</b>
						</text>
					) : null}
					<KitMarkdown content={props.body} fg={theme.textPrimary} />
				</box>
			</scrollbox>
			<Show
				when={selectionMenu()}
				fallback={<box position="absolute" width={0} height={0} />}
			>
				{(menu) => (
					<SelectionContextMenu
						x={menu().x}
						y={menu().y}
						containerWidth={containerRef?.width ?? renderer.terminalWidth}
						containerHeight={containerRef?.height ?? renderer.terminalHeight}
						zIndex={props.zIndex + 1}
						onCopy={copySelectedText}
						onQuote={quoteSelectedText}
						onClose={closeSelectionMenu}
					/>
				)}
			</Show>
		</box>
	);
}
