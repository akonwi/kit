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
import { theme } from "../../shell/theme";
import {
	FileTreeController,
	type FileTreeDirectoryHandle,
	type FileTreeItemHandle,
	type FileTreeVisibleRow,
} from "../../vendor/pierre-trees/index.js";
import type { ReviewFile } from "./model";
import { reviewStatusColor } from "./status";

// ── Types ───────────────────────────────────────────────────────────

type TreeMode = "changes" | "all";

export type FileTreePanelProps = {
	reviewFiles: ReviewFile[];
	allFiles: string[];
	focused: boolean;
	editorOpen: boolean;
	finderOpen: boolean;
	onFocusedPathChange: (path: string | null) => void;
	onSelectFile: (path: string) => void;
	onOpenFileFinder: () => void;
	onClose: () => void;
};

// ── Helpers ─────────────────────────────────────────────────────────

function getAncestorDirPaths(filePath: string): string[] {
	const parts = filePath.split("/");
	const dirs: string[] = [];
	for (let i = 1; i < parts.length; i++) {
		dirs.push(`${parts.slice(0, i).join("/")}/`);
	}
	return dirs;
}

function buildStatusColorMap(files: ReviewFile[]): Map<string, string> {
	const map = new Map<string, string>();
	for (const file of files) {
		map.set(file.path, reviewStatusColor(file));
	}
	return map;
}

function isDirectoryHandle(
	item: FileTreeItemHandle,
): item is FileTreeDirectoryHandle {
	return item.isDirectory();
}

// ── Component ───────────────────────────────────────────────────────

export function FileTreePanel(props: FileTreePanelProps) {
	const [treeMode, setTreeMode] = createSignal<TreeMode>(
		props.reviewFiles.length > 0 ? "changes" : "all",
	);
	const [treeVersion, setTreeVersion] = createSignal(0);

	let controller: FileTreeController | null = null;
	let controllerUnsub: (() => void) | null = null;
	let scrollRef: { scrollChildIntoView: (id: string) => void } | undefined;

	const statusColorMap = createMemo(() =>
		buildStatusColorMap(props.reviewFiles),
	);
	// Dedupe: a file that has both staged and unstaged changes appears twice
	// in reviewFiles (one entry per source). PathStore rejects duplicate paths.
	const changedPaths = createMemo(() => {
		const seen = new Set<string>();
		const paths: string[] = [];
		for (const file of props.reviewFiles) {
			if (seen.has(file.path)) continue;
			seen.add(file.path);
			paths.push(file.path);
		}
		return paths;
	});

	const changedExpandedDirs = createMemo(() => {
		const dirs = new Set<string>();
		for (const filePath of changedPaths()) {
			for (const dir of getAncestorDirPaths(filePath)) {
				dirs.add(dir);
			}
		}
		return [...dirs];
	});

	function resetTree() {
		controllerUnsub?.();
		controller?.destroy();
		const mode = treeMode();
		const rawPaths = mode === "changes" ? changedPaths() : props.allFiles;
		// FileTreeController's PathStore rejects duplicate paths; dedupe
		// defensively in case the inputs include the same path twice.
		const paths = Array.from(new Set(rawPaths));
		controller = new FileTreeController({
			paths,
			flattenEmptyDirectories: true,
			initialExpansion: mode === "changes" ? "closed" : "closed",
			initialExpandedPaths: changedExpandedDirs(),
			fileTreeSearchMode: "hide-non-matches",
		});
		controllerUnsub = controller.subscribe(() => setTreeVersion((v) => v + 1));
		setTreeVersion((v) => v + 1);
	}

	// Initialize and rebuild when inputs change
	createEffect(
		on([treeMode, changedPaths, () => props.allFiles], () => {
			resetTree();
		}),
	);

	onCleanup(() => {
		controllerUnsub?.();
		controller?.destroy();
	});

	// Notify parent when focused path changes
	createEffect(() => {
		treeVersion();
		const path = controller?.getFocusedPath() ?? null;
		// Strip trailing slash for directory paths
		const cleanPath = path?.endsWith("/") ? path.slice(0, -1) : path;
		props.onFocusedPathChange(cleanPath);
	});

	// Scroll focused row into view
	createEffect(() => {
		treeVersion();
		const path = controller?.getFocusedPath();
		if (!path) return;
		const idx = controller?.getFocusedIndex() ?? -1;
		if (idx >= 0) {
			scrollRef?.scrollChildIntoView(`file-tree-row-${idx}`);
		}
	});

	const visibleRows = createMemo<FileTreeVisibleRow[]>(() => {
		treeVersion();
		if (!controller) return [];
		const count = controller.getVisibleCount();
		if (count === 0) return [];
		return [...controller.getVisibleRows(0, count - 1)];
	});

	// ── Actions ───────────────────────────────────────────────────

	function getFocusedItem(): FileTreeItemHandle | null {
		const path = controller?.getFocusedPath();
		if (!path) return null;
		return controller?.getItem(path) ?? null;
	}

	function selectFocusedFile() {
		const item = getFocusedItem();
		if (!item) return;
		if (isDirectoryHandle(item)) {
			item.toggle();
			return;
		}
		const path = item.getPath();
		props.onSelectFile(path);
	}

	function toggleDir() {
		const item = getFocusedItem();
		if (item && isDirectoryHandle(item)) {
			item.toggle();
		}
	}

	function expandDir() {
		const item = getFocusedItem();
		if (item && isDirectoryHandle(item)) {
			if (!item.isExpanded()) {
				item.expand();
			} else {
				controller?.focusNextItem();
			}
		}
	}

	function collapseDir() {
		const item = getFocusedItem();
		if (item && isDirectoryHandle(item) && item.isExpanded()) {
			item.collapse();
			return;
		}
		controller?.focusParentItem();
	}

	function toggleTreeMode() {
		setTreeMode((m) => (m === "changes" ? "all" : "changes"));
	}

	function openFileFinder() {
		props.onOpenFileFinder();
	}

	// ── Keybindings ─────────────────────────────────────────────

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.focused && !props.editorOpen && !props.finderOpen,
		diagnosticsWhen: () => props.focused && !props.finderOpen,
		commands: {
			"review.close": props.onClose,
			"review.move-file-up": () => controller?.focusPreviousItem(),
			"review.move-file-down": () => controller?.focusNextItem(),
			"review.focus-file": selectFocusedFile,
			"review.toggle-file": toggleDir,
			"review.expand-dir": expandDir,
			"review.collapse-dir": collapseDir,
			"review.toggle-tree-mode": toggleTreeMode,
			"review.search-tree": openFileFinder,
		},
	}));

	// ── Render ───────────────────────────────────────────────────

	return (
		<box flexDirection="column" height="100%">
			{/* Mode toggle + search */}
			<box
				flexShrink={0}
				paddingX={1}
				height={1}
				flexDirection="row"
				justifyContent="space-between"
				gap={1}
			>
				<box flexDirection="row" gap={1}>
					<text
						fg={treeMode() === "changes" ? theme.textPrimary : theme.textMuted}
					>
						{treeMode() === "changes" ? "[changes]" : "changes"}
					</text>
					<text fg={treeMode() === "all" ? theme.textPrimary : theme.textMuted}>
						{treeMode() === "all" ? "[all files]" : "all files"}
					</text>
				</box>
			</box>

			{/* Tree rows */}
			<scrollbox
				ref={(el) => {
					scrollRef = el as typeof scrollRef;
				}}
				flexGrow={1}
				scrollY
				style={{
					scrollbarOptions: {
						trackOptions: {
							foregroundColor: theme.scrollbarFg,
							backgroundColor: theme.scrollbarBg,
						},
					},
				}}
			>
				<box flexDirection="column" gap={0} width="100%">
					<Show
						when={visibleRows().length > 0}
						fallback={
							<box paddingX={1} paddingY={1}>
								<text fg={theme.textMuted}>No files</text>
							</box>
						}
					>
						<For each={visibleRows()}>
							{(row) => (
								<FileTreeRow
									row={row}
									statusColor={statusColorMap().get(row.path) ?? null}
								/>
							)}
						</For>
					</Show>
				</box>
			</scrollbox>
		</box>
	);
}

// ── Row component ───────────────────────────────────────────────────

type FileTreeRowProps = {
	row: FileTreeVisibleRow;
	statusColor: string | null;
};

function FileTreeRow(props: FileTreeRowProps) {
	const indent = () => "  ".repeat(props.row.depth);
	const focused = () => props.row.isFocused;
	const bg = () => (focused() ? theme.pickerFocusedBg : theme.bgTransparent);

	const displayName = () => {
		const segs = props.row.flattenedSegments;
		if (segs && segs.length > 0) {
			return segs.map((s) => s.name).join("/");
		}
		return props.row.name;
	};

	const nameColor = () => {
		if (focused()) return theme.pickerFocusedText;
		if (props.statusColor) return props.statusColor;
		return theme.textPrimary;
	};

	const metaColor = () =>
		focused() ? theme.pickerFocusedText : theme.textMuted;

	return (
		<box
			id={`file-tree-row-${props.row.index}`}
			height={1}
			width="100%"
			overflow="hidden"
			backgroundColor={bg()}
			flexDirection="row"
		>
			<text fg={nameColor()} bg={bg()}>
				{indent()}
			</text>
			<Show when={props.row.kind === "directory"}>
				<text fg={metaColor()} bg={bg()}>
					{props.row.isExpanded ? TRIANGLE_DOWN : TRIANGLE_RIGHT}{" "}
				</text>
			</Show>
			<Show when={props.row.kind === "file"}>
				<text bg={bg()}>{"  "}</text>
			</Show>
			<text fg={nameColor()} bg={bg()}>
				{displayName()}
				{props.row.kind === "directory" ? "/" : ""}
			</text>
		</box>
	);
}
