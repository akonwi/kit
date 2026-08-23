import {
	closeSync,
	constants,
	fstatSync,
	openSync,
	readFileSync,
	realpathSync,
	statSync,
	unwatchFile,
	watch,
	watchFile,
} from "node:fs";
import path from "node:path";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	onCleanup,
	Show,
} from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import type { AttachmentsController } from "../../shell/attachments-controller";
import { inferFiletype } from "../../shell/filetype";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import {
	WorkspacePanelHeader,
	WorkspacePanelLayout,
	WorkspaceSidebarToggle,
} from "../../shell/WorkspacePanelLayout";
import type { ToastInput } from "../../state/toasts";
import type { FileCommentDraftController } from "./comment-draft-controller";
import {
	FileCommentView,
	fileContentRevision,
	fileReviewAttachmentId,
} from "./FileCommentView";
import {
	FILE_TREE_DRAWER_SPLIT_MIN_COLUMNS,
	FileTreeDrawer,
} from "./FileTreeDrawer";
import { listProjectFiles } from "./scan-files";

export type FileViewerPanelProps = {
	repoRoot: string;
	path: string;
	active: boolean;
	onOpenFile: (path: string) => void;
	onClose: () => void;
	attachments: AttachmentsController;
	commentDrafts: FileCommentDraftController;
	toast: (toast: ToastInput) => void;
	onSubmitMessage: () => void | Promise<void>;
	onFocusRequest?: () => void;
};

type FileLoadResult =
	| { content: string; error: null }
	| { content: null; error: string };

const MAX_FILE_BYTES = 1_000_000;
const MAX_FILE_LINES = 5_000;
const FILE_COMMENT_MOUSE_HINT = [{ key: "Click", action: "comment" }];

export function fileViewerHintGroup(drawerOpen: boolean): string {
	return drawerOpen ? "files" : "file-viewer";
}

export function resolveFileWithinRoot(
	repoRoot: string,
	relativePath: string,
): string | null {
	try {
		const root = realpathSync(path.resolve(repoRoot));
		const absolute = realpathSync(path.resolve(root, relativePath));
		return absolute === root || absolute.startsWith(`${root}${path.sep}`)
			? absolute
			: null;
	} catch {
		return null;
	}
}

export function readFileWithinRoot(
	repoRoot: string,
	relativePath: string,
): FileLoadResult {
	const absolute = resolveFileWithinRoot(repoRoot, relativePath);
	if (!absolute) return { content: null, error: "Could not read file" };
	let descriptor: number | undefined;
	try {
		descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
		const opened = fstatSync(descriptor);
		if (!opened.isFile()) {
			return { content: null, error: "Could not read file" };
		}
		if (opened.size > MAX_FILE_BYTES) {
			return { content: null, error: "File is too large to preview" };
		}

		// Revalidate the path after opening and compare it with the descriptor.
		// Reads use the descriptor, so a later path swap cannot redirect content.
		const currentAbsolute = resolveFileWithinRoot(repoRoot, relativePath);
		if (currentAbsolute !== absolute) {
			return { content: null, error: "Could not read file" };
		}
		const current = statSync(currentAbsolute);
		if (current.dev !== opened.dev || current.ino !== opened.ino) {
			return { content: null, error: "Could not read file" };
		}

		const content = readFileSync(descriptor, "utf8");
		if (content.includes("\0")) {
			return { content: null, error: "Binary files cannot be previewed" };
		}
		if (content.split("\n", MAX_FILE_LINES + 1).length > MAX_FILE_LINES) {
			return { content: null, error: "File has too many lines to preview" };
		}
		return { content, error: null };
	} catch {
		return { content: null, error: "Could not read file" };
	} finally {
		if (descriptor !== undefined) closeSync(descriptor);
	}
}

export function FileViewerPanel(props: FileViewerPanelProps) {
	const [drawerOpen, setDrawerOpen] = createSignal(false);
	const [contentWidth, setContentWidth] = createSignal(80);
	const [commentEditing, setCommentEditing] = createSignal(false);
	const [commentsStale, setCommentsStale] = createSignal(false);
	const [noteCount, setNoteCount] = createSignal(0);
	const [fileRevision, setFileRevision] = createSignal(0);
	let contentRef: { width: number } | undefined;

	const loadedFile = createMemo(() => {
		fileRevision();
		return readFileWithinRoot(props.repoRoot, props.path);
	});
	createEffect(() => {
		if (!props.active) return;
		const absolute = resolveFileWithinRoot(props.repoRoot, props.path);
		if (!absolute) return;
		const refresh = () => setFileRevision((revision) => revision + 1);
		refresh();
		watchFile(absolute, { interval: 500 }, refresh);
		onCleanup(() => unwatchFile(absolute, refresh));
	});
	const lines = createMemo(() => {
		const value = loadedFile().content;
		if (value === null) return [];
		return value.replace(/\r\n/g, "\n").split("\n");
	});
	function currentFileRevision(): string | null {
		const result = readFileWithinRoot(props.repoRoot, props.path);
		if (result.content === null) return null;
		return fileContentRevision(
			result.content.replace(/\r\n/g, "\n").split("\n"),
		);
	}
	createEffect(() => {
		if (loadedFile().content !== null) return;
		props.attachments.detach(
			fileReviewAttachmentId(props.repoRoot, props.path),
			"pending",
		);
	});
	const filetype = createMemo(() => inferFiletype(props.path));
	const lineNumberWidth = createMemo(() =>
		Math.max(1, String(lines().length).length),
	);
	const drawerUsesFullWidth = createMemo(
		() => contentWidth() < FILE_TREE_DRAWER_SPLIT_MIN_COLUMNS,
	);
	const drawerWidth = createMemo(() =>
		Math.max(24, Math.min(36, Math.floor(contentWidth() * 0.4))),
	);
	const viewerWidth = createMemo(() =>
		Math.max(
			1,
			contentWidth() -
				(drawerOpen() && !drawerUsesFullWidth() ? drawerWidth() : 0),
		),
	);
	const codeColumns = createMemo(() =>
		Math.max(10, viewerWidth() - lineNumberWidth() - 3),
	);
	const [projectFiles, { refetch: refetchProjectFiles }] = createResource(
		() => (props.active && drawerOpen() ? props.repoRoot : null),
		async (root) => (root ? listProjectFiles(root) : []),
	);
	createEffect(() => {
		if (!props.active || !drawerOpen()) return;
		let refreshTimeout: ReturnType<typeof setTimeout> | undefined;
		let watcher: ReturnType<typeof watch> | undefined;
		try {
			watcher = watch(
				props.repoRoot,
				{ recursive: true },
				(_event, changedPath) => {
					const relativePath = String(changedPath ?? "").replaceAll("\\", "/");
					if (
						relativePath === "node_modules" ||
						relativePath.startsWith("node_modules/") ||
						relativePath.startsWith(".git/")
					) {
						return;
					}
					if (refreshTimeout) clearTimeout(refreshTimeout);
					refreshTimeout = setTimeout(() => void refetchProjectFiles(), 500);
				},
			);
		} catch {
			// The drawer remains usable on platforms without recursive watching.
		}
		onCleanup(() => {
			watcher?.close();
			if (refreshTimeout) clearTimeout(refreshTimeout);
		});
	});

	useKeymapLayer(() => ({
		scope: "modal",
		when: () => props.active && !drawerOpen() && !commentEditing(),
		diagnosticsWhen: () => props.active && !drawerOpen() && !commentEditing(),
		commands: {
			"file-viewer.close": props.onClose,
		},
	}));

	const header = (
		<WorkspacePanelHeader
			leading={
				<WorkspaceSidebarToggle
					expanded={drawerOpen()}
					onToggle={() => {
						if (!commentEditing()) setDrawerOpen((open) => !open);
					}}
				/>
			}
			left={<text fg={theme.textPrimary}>{props.path}</text>}
			right={
				<Show when={loadedFile().content !== null}>
					<text fg={commentsStale() ? theme.warningText : theme.textMuted}>
						{lines().length} line{lines().length === 1 ? "" : "s"}
						{noteCount() > 0
							? ` · ${noteCount()} note${noteCount() === 1 ? "" : "s"}`
							: ""}
						{commentsStale() ? " · stale" : ""}
					</text>
				</Show>
			}
		/>
	);

	return (
		<WorkspacePanelLayout
			header={header}
			footer={
				<KeymapHintBar
					borderless
					group={fileViewerHintGroup(drawerOpen())}
					prefixBindings={
						!drawerOpen() && !commentEditing() && loadedFile().content !== null
							? FILE_COMMENT_MOUSE_HINT
							: undefined
					}
				/>
			}
		>
			<box
				ref={(value) => {
					contentRef = value as typeof contentRef;
				}}
				onSizeChange={() => {
					const width = contentRef?.width ?? 0;
					if (width > 0) setContentWidth(width);
				}}
				flexGrow={1}
				flexDirection="row"
				overflow="hidden"
			>
				<Show when={drawerOpen()}>
					<box
						width={drawerUsesFullWidth() ? "100%" : drawerWidth()}
						flexShrink={0}
						border={["right"]}
						borderColor={theme.borderDefault}
					>
						<FileTreeDrawer
							paths={projectFiles() ?? []}
							focused={props.active && drawerOpen()}
							selectedPath={props.path}
							onSelectFile={props.onOpenFile}
							onClose={() => setDrawerOpen(false)}
							onFocusRequest={props.onFocusRequest}
						/>
					</box>
				</Show>
				<Show when={!drawerOpen() || !drawerUsesFullWidth()}>
					<box flexGrow={1} overflow="hidden" backgroundColor={theme.bg}>
						<Show
							when={loadedFile().content !== null}
							fallback={
								<box flexGrow={1} alignItems="center" justifyContent="center">
									<text fg={theme.textMuted}>{loadedFile().error}</text>
								</box>
							}
						>
							<FileCommentView
								repoRoot={props.repoRoot}
								path={props.path}
								lines={lines()}
								filetype={filetype()}
								contentColumns={codeColumns()}
								lineNumberWidth={lineNumberWidth()}
								active={props.active && !drawerOpen()}
								attachments={props.attachments}
								commentDrafts={props.commentDrafts}
								toast={props.toast}
								onSubmitMessage={props.onSubmitMessage}
								getCurrentRevision={currentFileRevision}
								onFocusRequest={props.onFocusRequest}
								onEditingChange={setCommentEditing}
								onNoteCountChange={setNoteCount}
								onStaleChange={setCommentsStale}
							/>
						</Show>
					</box>
				</Show>
			</box>
		</WorkspacePanelLayout>
	);
}
