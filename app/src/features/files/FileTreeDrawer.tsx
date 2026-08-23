import type { MouseEvent as TuiMouseEvent } from "@opentui/core";
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	on,
	onCleanup,
	Show,
} from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { TRIANGLE_DOWN, TRIANGLE_RIGHT } from "../../shell/glyphs";
import { scrollbarStyle, theme } from "../../shell/theme";
import {
	FileTreeController,
	type FileTreeDirectoryHandle,
	type FileTreeItemHandle,
	type FileTreeVisibleRow,
} from "../../vendor/pierre-trees/index.js";

export const FILE_TREE_DRAWER_SPLIT_MIN_COLUMNS = 72;

export type FileTreeDrawerProps = {
	paths: readonly string[];
	focused: boolean;
	selectedPath?: string | null;
	initialExpandedPaths?: readonly string[];
	colorForPath?: (path: string) => string | undefined;
	onSelectFile: (path: string) => void;
	onClose: () => void;
	onFocusRequest?: () => void;
};

function isDirectoryHandle(
	item: FileTreeItemHandle,
): item is FileTreeDirectoryHandle {
	return item.isDirectory();
}

function ancestorDirectories(filePath: string): string[] {
	const parts = filePath.split("/");
	const directories: string[] = [];
	for (let index = 1; index < parts.length; index += 1) {
		directories.push(`${parts.slice(0, index).join("/")}/`);
	}
	return directories;
}

function visibleRowsEqual(
	current: FileTreeVisibleRow,
	next: FileTreeVisibleRow,
): boolean {
	return (
		current.path === next.path &&
		current.name === next.name &&
		current.kind === next.kind &&
		current.depth === next.depth &&
		current.index === next.index &&
		current.isFocused === next.isFocused &&
		current.isSelected === next.isSelected &&
		current.isExpanded === next.isExpanded &&
		current.isFlattened === next.isFlattened
	);
}

export function FileTreeDrawer(props: FileTreeDrawerProps) {
	const [version, setVersion] = createSignal(0);
	let controller: FileTreeController | null = null;
	let unsubscribe: (() => void) | null = null;
	let scrollRef: { scrollChildIntoView?: (id: string) => void } | undefined;

	const paths = createMemo<string[]>((previous) => {
		const next = Array.from(new Set(props.paths));
		return previous.length === next.length &&
			previous.every((value, index) => value === next[index])
			? previous
			: next;
	}, []);

	function rebuild(): void {
		const previousController = controller;
		const previousFocusedPath = previousController?.getFocusedPath() ?? null;
		const previousExpandedPaths = previousController
			? [
					...previousController.getVisibleRows(
						0,
						Math.max(0, previousController.getVisibleCount() - 1),
					),
				]
					.filter((row) => row.kind === "directory" && row.isExpanded)
					.map((row) => row.path)
			: [...(props.initialExpandedPaths ?? [])];
		unsubscribe?.();
		previousController?.destroy();
		controller = new FileTreeController({
			paths: paths(),
			flattenEmptyDirectories: true,
			initialExpansion: "closed",
			initialExpandedPaths: previousExpandedPaths,
			fileTreeSearchMode: "hide-non-matches",
		});
		const focusPath =
			(previousFocusedPath && controller.getItem(previousFocusedPath)
				? previousFocusedPath
				: props.selectedPath) ?? null;
		if (focusPath && controller.getItem(focusPath)) {
			for (const directory of ancestorDirectories(focusPath)) {
				const item = controller.getItem(directory);
				if (item && isDirectoryHandle(item) && !item.isExpanded())
					item.expand();
			}
			controller.focusPath(focusPath);
		}
		unsubscribe = controller.subscribe(() =>
			setVersion((current) => current + 1),
		);
		setVersion((current) => current + 1);
	}

	createEffect(on(paths, rebuild));
	onCleanup(() => {
		unsubscribe?.();
		controller?.destroy();
	});

	createEffect(
		on(
			() => props.selectedPath,
			(selectedPath) => {
				if (!selectedPath || !controller?.getItem(selectedPath)) return;
				for (const directory of ancestorDirectories(selectedPath)) {
					const item = controller.getItem(directory);
					if (item && isDirectoryHandle(item) && !item.isExpanded())
						item.expand();
				}
				controller.focusPath(selectedPath);
			},
			{ defer: true },
		),
	);

	createEffect(() => {
		version();
		const index = controller?.getFocusedIndex() ?? -1;
		if (index >= 0)
			scrollRef?.scrollChildIntoView?.(`file-drawer-row-${index}`);
	});

	let previousRowsByPath = new Map<string, FileTreeVisibleRow>();
	const rows = createMemo<FileTreeVisibleRow[]>(() => {
		version();
		if (!controller) return [];
		const count = controller.getVisibleCount();
		if (count === 0) return [];
		const next = [...controller.getVisibleRows(0, count - 1)].map((row) => {
			const previous = previousRowsByPath.get(row.path);
			return previous && visibleRowsEqual(previous, row) ? previous : row;
		});
		previousRowsByPath = new Map(next.map((row) => [row.path, row]));
		return next;
	});

	function focusedItem(): FileTreeItemHandle | null {
		const focusedPath = controller?.getFocusedPath();
		return focusedPath ? (controller?.getItem(focusedPath) ?? null) : null;
	}

	function activateFocused(): void {
		const item = focusedItem();
		if (!item) return;
		if (isDirectoryHandle(item)) item.toggle();
		else props.onSelectFile(item.getPath());
	}

	function expandFocused(): void {
		const item = focusedItem();
		if (item && isDirectoryHandle(item)) {
			if (!item.isExpanded()) item.expand();
			else controller?.focusNextItem();
		}
	}

	function collapseFocused(): void {
		const item = focusedItem();
		if (item && isDirectoryHandle(item) && item.isExpanded()) item.collapse();
		else controller?.focusParentItem();
	}

	function selectPath(path: string, event: TuiMouseEvent): void {
		if (event.button !== 0) return;
		event.preventDefault();
		event.stopPropagation();
		props.onFocusRequest?.();
		controller?.focusPath(path);
		const item = controller?.getItem(path);
		if (!item) return;
		if (isDirectoryHandle(item)) item.toggle();
		else props.onSelectFile(item.getPath());
	}

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.focused,
		diagnosticsWhen: () => props.focused,
		commands: {
			"file-tree.close": props.onClose,
			"file-tree.move-up": () => controller?.focusPreviousItem(),
			"file-tree.move-down": () => controller?.focusNextItem(),
			"file-tree.open": activateFocused,
			"file-tree.toggle": activateFocused,
			"file-tree.expand": expandFocused,
			"file-tree.collapse": collapseFocused,
		},
	}));

	return (
		<scrollbox
			ref={(value) => {
				scrollRef = value as typeof scrollRef;
			}}
			flexGrow={1}
			scrollY
			style={scrollbarStyle()}
		>
			<box flexDirection="column" width="100%">
				<Show
					when={rows().length > 0}
					fallback={
						<box paddingX={1}>
							<text fg={theme.textMuted}>No files</text>
						</box>
					}
				>
					<For each={rows()}>
						{(row) => {
							const focused = () => row.isFocused;
							const background = () =>
								focused() ? theme.pickerFocusedBg : theme.bgTransparent;
							const color = () =>
								focused()
									? theme.pickerFocusedText
									: (props.colorForPath?.(row.path) ?? theme.textPrimary);
							return (
								<box
									id={`file-drawer-row-${row.index}`}
									height={1}
									width="100%"
									overflow="hidden"
									backgroundColor={background()}
									onMouseDown={(event) => selectPath(row.path, event)}
								>
									<text fg={color()} bg={background()}>
										{"  ".repeat(row.depth)}
										{row.kind === "directory"
											? `${row.isExpanded ? TRIANGLE_DOWN : TRIANGLE_RIGHT} `
											: "  "}
										{row.flattenedSegments
											?.map((segment) => segment.name)
											.join("/") ?? row.name}
										{row.kind === "directory" ? "/" : ""}
									</text>
								</box>
							);
						}}
					</For>
				</Show>
			</box>
		</scrollbox>
	);
}
