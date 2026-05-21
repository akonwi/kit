import { useBindings, useKeymap } from "@opentui/keymap/solid";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	For,
	Show,
} from "solid-js";
import {
	type CommandBindingDefinition,
	createConfiguredCommandBindingResult,
	createKeymapCommands,
	withKitKeyAliases,
} from "../../keymap/bindings";
import {
	createKeybindingDiagnosticReporter,
	reportKeybindingDiagnostics,
} from "../../keymap/diagnostics";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { SessionSummary } from "../../session";
import { readSession, updateSession } from "../../session";
import { Dialog } from "../../shell/Dialog";
import { ELLIPSIS } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";
import type { ToastInput } from "../../state/toasts";
import { formatTimeAgo } from "../commands/utils";
import {
	buildSessionForest,
	findSessionRowIndex,
	flattenSessionForest,
	formatSessionTreePrefix,
	getSessionTreeTitle,
} from "./tree";

export type SessionExplorerModalProps = {
	runtime: AgentRuntime;
	toast: (toast: ToastInput) => void;
	onClose: () => void;
	onSelect: (sessionId: string | null) => void;
};

const MAX_VISIBLE_ROWS = 18;
const TITLE_COLUMN_WIDTH = 48;
const MIN_TITLE_COLUMN_WIDTH = 24;
const SESSION_ID_COLUMN_WIDTH = 8;
const UPDATED_COLUMN_WIDTH = 10;
const CWD_COLUMN_WIDTH = 34;
const MIN_CWD_COLUMN_WIDTH = 20;
const DEFAULT_ROW_WIDTH =
	TITLE_COLUMN_WIDTH +
	SESSION_ID_COLUMN_WIDTH +
	UPDATED_COLUMN_WIDTH +
	CWD_COLUMN_WIDTH +
	3;

type Mode = "navigate" | "rename" | "confirmDelete" | "confirmSquash";
type SquashTarget = "parent" | "current";

function formatCwd(cwd: string): string {
	const home = process.env.HOME || process.env.USERPROFILE || "";
	return home && cwd.startsWith(home) ? `~${cwd.slice(home.length)}` : cwd;
}

function sessionId(session: SessionSummary): string {
	return session.id.slice(0, SESSION_ID_COLUMN_WIDTH);
}

function sessionUpdated(session: SessionSummary): string {
	return formatTimeAgo(new Date(session.updatedAt));
}

function sessionCount(count: number): string {
	return `${count} session${count === 1 ? "" : "s"}`;
}

function truncateText(text: string, maxLength: number): string {
	if (text.length <= maxLength) return text;
	return `${text.slice(0, Math.max(0, maxLength - 1))}${ELLIPSIS}`;
}

function countVisibleColumns(columns: boolean[]): number {
	return columns.filter(Boolean).length;
}

function visibleColumnWidth(show: boolean, width: number): number {
	return show ? width : 0;
}

export function SessionExplorerModal(props: SessionExplorerModalProps) {
	const keymap = useKeymap();
	const reportKeybindingDiagnostic = createKeybindingDiagnosticReporter(
		props.toast,
	);
	const currentSessionId = () => props.runtime.getSession().id;
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [lastCenteredSessionId, setLastCenteredSessionId] = createSignal<
		string | null
	>(null);
	const [confirmSquashSession, setConfirmSquashSession] =
		createSignal<SessionSummary | null>(null);
	const [confirmSquashTarget, setConfirmSquashTarget] =
		createSignal<SquashTarget | null>(null);
	const [renameSession, setRenameSession] = createSignal<SessionSummary | null>(
		null,
	);
	const [deleteSession, setDeleteSession] = createSignal<SessionSummary | null>(
		null,
	);
	const [renameText, setRenameText] = createSignal("");
	const [sessions, { mutate }] = createResource(async () =>
		props.runtime.listAllSessions(),
	);

	let renameRef:
		| { plainText: string; setText: (value: string) => void }
		| undefined;
	let scrollRef:
		| { scrollTo: (opts: { x?: number; y?: number } | number) => void }
		| undefined;
	let listRef: { width: number } | undefined;
	const [listWidth, setListWidth] = createSignal(DEFAULT_ROW_WIDTH);

	function syncListWidth() {
		const width = listRef?.width ?? 0;
		if (width > 0) setListWidth(width);
	}

	const mode = createMemo<Mode>(() => {
		if (confirmSquashSession()) return "confirmSquash";
		if (deleteSession()) return "confirmDelete";
		if (renameSession()) return "rename";
		return "navigate";
	});

	const rows = createMemo(() => {
		const roots = buildSessionForest(sessions() ?? []);
		return flattenSessionForest(roots, currentSessionId());
	});

	const currentSelectionIndex = createMemo(() =>
		findSessionRowIndex(rows(), currentSessionId()),
	);
	const selectedRow = createMemo(() => rows()[selectedIndex()] ?? null);
	const selectedSessionId = createMemo(() => selectedRow()?.session.id ?? null);
	const selectedSquashTarget = createMemo<SquashTarget | null>(() => {
		const row = selectedRow();
		if (!row) return null;
		if (row.isCurrent && row.session.parentSessionId) return "parent";
		if (!row.isCurrent && row.session.parentSessionId === currentSessionId()) {
			return "current";
		}
		return null;
	});
	const selectedSessionCanSquash = createMemo(
		() => selectedSquashTarget() !== null,
	);
	const selectedSessionCanDelete = createMemo(() =>
		Boolean(selectedRow() && !selectedRow()?.isCurrent),
	);
	const rowColumns = createMemo(() => {
		const width = listWidth();
		const showCwd = width >= 80;
		const showId = width >= 52;
		const showUpdated = width >= 40;
		const cwdWidth = showCwd
			? Math.min(
					CWD_COLUMN_WIDTH,
					Math.max(
						MIN_CWD_COLUMN_WIDTH,
						width -
							MIN_TITLE_COLUMN_WIDTH -
							visibleColumnWidth(showId, SESSION_ID_COLUMN_WIDTH) -
							visibleColumnWidth(showUpdated, UPDATED_COLUMN_WIDTH) -
							3,
					),
				)
			: 0;
		const visibleMetadataColumns = countVisibleColumns([
			showId,
			showUpdated,
			showCwd,
		]);
		const metadataWidth =
			visibleColumnWidth(showId, SESSION_ID_COLUMN_WIDTH) +
			visibleColumnWidth(showUpdated, UPDATED_COLUMN_WIDTH) +
			cwdWidth;

		return {
			showCwd,
			showId,
			showUpdated,
			cwdWidth,
			titleWidth: Math.max(1, width - metadataWidth - visibleMetadataColumns),
		};
	});

	createEffect(() => {
		const allRows = rows();
		if (allRows.length === 0) {
			setSelectedIndex(0);
			setLastCenteredSessionId(null);
			return;
		}

		const currentId = currentSessionId();
		const currentIndex = currentSelectionIndex();
		if (currentIndex >= 0 && lastCenteredSessionId() !== currentId) {
			setSelectedIndex(currentIndex);
			setLastCenteredSessionId(currentId);
			return;
		}

		setSelectedIndex((index) =>
			Math.max(0, Math.min(index, allRows.length - 1)),
		);
	});

	createEffect(() => {
		rows();
		scrollRef?.scrollTo({
			x: 0,
			y: Math.max(0, selectedIndex() - Math.floor(MAX_VISIBLE_ROWS / 2)),
		});
	});

	function clampIndex(nextCount: number) {
		setSelectedIndex((index) => Math.max(0, Math.min(index, nextCount - 1)));
	}

	function beginRename() {
		const session = selectedRow()?.session;
		if (!session) return;
		setRenameSession(session);
		setRenameText(session.name ?? "");
		setTimeout(() => renameRef?.setText(session.name ?? ""), 0);
	}

	function beginDelete() {
		const session = selectedRow()?.session;
		if (!session) return;
		if (session.id === currentSessionId()) {
			props.toast({
				title: "Cannot delete active session",
				subtitle: "Switch to another session before deleting this one.",
				variant: "warning",
			});
			return;
		}
		setDeleteSession(session);
	}

	async function handleRenameSubmit() {
		const target = renameSession();
		if (!target) return;
		const newName = renameText().trim();
		setRenameSession(null);
		if (!newName) return;
		const session = await readSession(target.id);
		if (!session) {
			props.toast({
				title: "Rename failed",
				subtitle: "Session could not be found.",
				variant: "error",
			});
			return;
		}
		await updateSession(session, { name: newName });
		mutate((current) =>
			(current ?? []).map((item) =>
				item.id === target.id ? { ...item, name: newName } : item,
			),
		);
	}

	async function handleDeleteConfirm() {
		const target = deleteSession();
		if (!target) return;
		setDeleteSession(null);
		try {
			await props.runtime.deleteSession(target.id);
			mutate((current) => {
				const next = (current ?? []).filter((item) => item.id !== target.id);
				clampIndex(next.length);
				return next;
			});
		} catch (error) {
			props.toast({
				title: "Delete failed",
				subtitle: String(error),
				variant: "error",
			});
		}
	}

	function beginSquash() {
		const session = selectedRow()?.session;
		const target = selectedSquashTarget();
		if (!session || !target) return;
		setConfirmSquashSession(session);
		setConfirmSquashTarget(target);
	}

	function cancelSquashConfirm() {
		setConfirmSquashSession(null);
		setConfirmSquashTarget(null);
	}

	function handleSquashConfirm() {
		const session = confirmSquashSession();
		const target = confirmSquashTarget();
		cancelSquashConfirm();
		if (!session || !target) return;
		props.onClose();
		const squash =
			target === "current"
				? props.runtime.mergeChildIntoCurrent(session.id)
				: props.runtime.mergeUp();
		void squash.catch((error) => {
			props.toast({
				title: "Squash failed",
				subtitle: String(error),
				variant: "error",
			});
		});
	}

	const navigateBaseCommands = [
		{
			binding: {
				cmd: "session-explorer.close",
				key: ["escape", "ctrl+c"],
				desc: "Close session explorer",
				group: "session-explorer",
			},
			command: { hint: "close", run: props.onClose },
		},
		{
			binding: {
				cmd: "session-explorer.select",
				key: "return",
				desc: "Switch to selected session",
				group: "session-explorer",
			},
			command: {
				hint: "switch",
				run: () => props.onSelect(selectedSessionId()),
			},
		},
		{
			binding: {
				cmd: "session-explorer.move-up",
				key: ["up", "k"],
				desc: "Move to previous session",
				group: "session-explorer",
			},
			command: {
				hint: "move",
				run: () => {
					setSelectedIndex((index) => Math.max(0, index - 1));
				},
			},
		},
		{
			binding: {
				cmd: "session-explorer.move-down",
				key: ["down", "j"],
				desc: "Move to next session",
				group: "session-explorer",
			},
			command: {
				hint: "move",
				run: () => {
					setSelectedIndex((index) => Math.min(rows().length - 1, index + 1));
				},
			},
		},
		{
			binding: {
				cmd: "session-explorer.page-up",
				key: "pageup",
				desc: "Scroll sessions up",
				group: "session-explorer",
			},
			command: {
				hint: "scroll",
				run: () => {
					setSelectedIndex((index) => Math.max(0, index - MAX_VISIBLE_ROWS));
				},
			},
		},
		{
			binding: {
				cmd: "session-explorer.page-down",
				key: "pagedown",
				desc: "Scroll sessions down",
				group: "session-explorer",
			},
			command: {
				hint: "scroll",
				run: () => {
					setSelectedIndex((index) =>
						Math.min(rows().length - 1, index + MAX_VISIBLE_ROWS),
					);
				},
			},
		},
		{
			binding: {
				cmd: "session-explorer.rename",
				key: "r",
				desc: "Rename selected session",
				group: "session-explorer",
			},
			command: { hint: "rename", run: beginRename },
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const navigateDeleteCommands = [
		{
			binding: {
				cmd: "session-explorer.delete",
				key: "ctrl+d",
				desc: "Delete selected session",
				group: "session-explorer",
			},
			command: { hint: "delete", run: beginDelete },
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const navigateSquashCommands = [
		{
			binding: {
				cmd: "session-explorer.squash",
				key: "s",
				desc: "Squash selected session",
				group: "session-explorer",
			},
			command: { hint: "squash", run: beginSquash },
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const renameCommands = [
		{
			binding: {
				cmd: "session-explorer.rename-save",
				key: "return",
				desc: "Save session name",
				group: "session-explorer",
			},
			command: {
				hint: "save",
				run: () => void handleRenameSubmit(),
			},
		},
		{
			binding: {
				cmd: "session-explorer.rename-cancel",
				key: ["escape", "ctrl+c"],
				desc: "Cancel session rename",
				group: "session-explorer",
			},
			command: {
				hint: "cancel",
				run: () => {
					setRenameSession(null);
				},
			},
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const confirmCommands = [
		{
			binding: {
				cmd: "session-explorer.confirm",
				key: "return",
				desc: "Confirm session action",
				group: "session-explorer",
			},
			command: { hint: "confirm", run: () => {} },
		},
		{
			binding: {
				cmd: "session-explorer.cancel",
				key: ["escape", "ctrl+c"],
				desc: "Cancel session action",
				group: "session-explorer",
			},
			command: { hint: "cancel", run: () => {} },
		},
	] as const satisfies readonly CommandBindingDefinition[];
	const navigateBaseBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			navigateBaseCommands,
			props.runtime.settings.keybindings,
		),
	);
	const navigateDeleteBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			navigateDeleteCommands,
			props.runtime.settings.keybindings,
		),
	);
	const navigateSquashBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			navigateSquashCommands,
			props.runtime.settings.keybindings,
		),
	);
	const renameBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			renameCommands,
			props.runtime.settings.keybindings,
		),
	);
	const confirmBindings = createMemo(() =>
		createConfiguredCommandBindingResult(
			keymap,
			confirmCommands,
			props.runtime.settings.keybindings,
		),
	);

	createEffect(() => {
		reportKeybindingDiagnostics(
			[
				...navigateBaseBindings().diagnostics,
				...navigateDeleteBindings().diagnostics,
				...navigateSquashBindings().diagnostics,
				...renameBindings().diagnostics,
				...confirmBindings().diagnostics,
			],
			reportKeybindingDiagnostic,
		);
	});

	useBindings(() =>
		withKitKeyAliases({
			enabled: () => mode() === "navigate",
			priority: 200,
			commands: createKeymapCommands(navigateBaseCommands),
			bindings: navigateBaseBindings().bindings,
		}),
	);
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => mode() === "navigate" && selectedSessionCanDelete(),
			priority: 200,
			commands: createKeymapCommands(navigateDeleteCommands),
			bindings: navigateDeleteBindings().bindings,
		}),
	);
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => mode() === "navigate" && selectedSessionCanSquash(),
			priority: 200,
			commands: createKeymapCommands(navigateSquashCommands),
			bindings: navigateSquashBindings().bindings,
		}),
	);
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => mode() === "rename",
			priority: 200,
			commands: createKeymapCommands(renameCommands),
			bindings: renameBindings().bindings,
		}),
	);
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => mode() === "confirmDelete",
			priority: 200,
			commands: createKeymapCommands([
				{
					...confirmCommands[0],
					command: {
						...confirmCommands[0].command,
						run: () => void handleDeleteConfirm(),
					},
				},
				{
					...confirmCommands[1],
					command: {
						...confirmCommands[1].command,
						run: () => {
							setDeleteSession(null);
						},
					},
				},
			]),
			bindings: confirmBindings().bindings,
		}),
	);
	useBindings(() =>
		withKitKeyAliases({
			enabled: () => mode() === "confirmSquash",
			priority: 200,
			commands: createKeymapCommands([
				{
					...confirmCommands[0],
					command: { ...confirmCommands[0].command, run: handleSquashConfirm },
				},
				{
					...confirmCommands[1],
					command: { ...confirmCommands[1].command, run: cancelSquashConfirm },
				},
			]),
			bindings: confirmBindings().bindings,
		}),
	);

	return (
		<Dialog.Root width="85%" maxWidth={120} minWidth={44} height="55%">
			<Dialog.Header>
				<Dialog.Title>Session Explorer</Dialog.Title>
				<Dialog.Meta>{sessionCount(rows().length)}</Dialog.Meta>
			</Dialog.Header>

			<Dialog.Body>
				<Show
					when={!sessions.loading}
					fallback={<text fg={theme.textMuted}>Loading sessions…</text>}
				>
					<Show
						when={rows().length > 0}
						fallback={<text fg={theme.textMuted}>No sessions found.</text>}
					>
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
							<box
								ref={(el) => {
									listRef = el as typeof listRef;
									queueMicrotask(syncListWidth);
								}}
								onSizeChange={syncListWidth}
								flexDirection="column"
								gap={0}
								width="100%"
							>
								<For each={rows()}>
									{(row) => {
										const prefix = formatSessionTreePrefix(row);
										const titleWidth = () =>
											Math.max(
												1,
												Math.min(TITLE_COLUMN_WIDTH, rowColumns().titleWidth) -
													prefix.length,
											);
										const focused = () =>
											row.session.id === selectedSessionId();
										const rowBg = () =>
											focused() ? theme.pickerFocusedBg : theme.bgTransparent;
										const labelColor = () =>
											row.isCurrent
												? focused()
													? theme.userTextFocused
													: theme.userText
												: focused()
													? theme.pickerFocusedText
													: theme.textPrimary;
										const metaColor = () =>
											focused() ? theme.pickerFocusedText : theme.textMuted;
										return (
											<box
												flexDirection="row"
												width="100%"
												height={1}
												overflow="hidden"
												gap={1}
												backgroundColor={rowBg()}
											>
												<box
													flexGrow={1}
													flexShrink={1}
													minWidth={MIN_TITLE_COLUMN_WIDTH}
													flexDirection="row"
													height={1}
													overflow="hidden"
												>
													<Show when={prefix.length > 0}>
														<text fg={metaColor()} bg={rowBg()}>
															{prefix}
														</text>
													</Show>
													<text fg={labelColor()} bg={rowBg()}>
														{truncateText(
															getSessionTreeTitle(row),
															titleWidth(),
														)}
													</text>
												</box>
												<Show when={rowColumns().showId}>
													<box
														flexShrink={0}
														width={SESSION_ID_COLUMN_WIDTH}
														height={1}
													>
														<text fg={metaColor()} bg={rowBg()}>
															{sessionId(row.session)}
														</text>
													</box>
												</Show>
												<Show when={rowColumns().showUpdated}>
													<box
														flexShrink={0}
														width={UPDATED_COLUMN_WIDTH}
														height={1}
														justifyContent="flex-end"
													>
														<text fg={metaColor()} bg={rowBg()}>
															{sessionUpdated(row.session)}
														</text>
													</box>
												</Show>
												<Show when={rowColumns().showCwd}>
													<box
														flexShrink={1}
														width={rowColumns().cwdWidth}
														height={1}
														overflow="hidden"
														justifyContent="flex-end"
													>
														<text fg={metaColor()} bg={rowBg()}>
															{formatCwd(row.session.cwd)}
														</text>
													</box>
												</Show>
											</box>
										);
									}}
								</For>
							</box>
						</scrollbox>
					</Show>
				</Show>
			</Dialog.Body>

			<Dialog.Footer paddingTop={1}>
				<KeymapHintBar borderless group="session-explorer" />
			</Dialog.Footer>

			<Show when={renameSession()}>
				{(session) => (
					<Dialog.Root maxWidth={80}>
						<Dialog.Header>
							<Dialog.Title>
								Rename "{session().name?.trim() || session().id.slice(0, 8)}"
							</Dialog.Title>
						</Dialog.Header>
						<textarea
							ref={(el) => {
								renameRef = el as typeof renameRef;
							}}
							minHeight={1}
							maxHeight={1}
							placeholder="Enter new session name..."
							placeholderColor={theme.textPlaceholder}
							backgroundColor={theme.bg}
							focusedBackgroundColor={theme.bgSurface}
							textColor={theme.textPrimary}
							focusedTextColor={theme.textPrimary}
							cursorColor={theme.cursor}
							showCursor
							focused
							onContentChange={() => setRenameText(renameRef?.plainText ?? "")}
						/>
						<Dialog.Footer>
							<KeymapHintBar borderless group="session-explorer" />
						</Dialog.Footer>
					</Dialog.Root>
				)}
			</Show>

			<Show when={deleteSession()}>
				{(session) => (
					<Dialog.Root maxWidth={80}>
						<Dialog.Header>
							<Dialog.Title fg={theme.errorText}>
								Delete "{session().name?.trim() || session().id.slice(0, 8)}"?
							</Dialog.Title>
						</Dialog.Header>
						<Dialog.Footer>
							<KeymapHintBar borderless group="session-explorer" />
						</Dialog.Footer>
					</Dialog.Root>
				)}
			</Show>

			<Show when={confirmSquashSession()}>
				{(session) => {
					const targetLabel = () =>
						confirmSquashTarget() === "current"
							? "the current session"
							: "its parent";
					return (
						<Dialog.Root maxWidth={80}>
							<Dialog.Header>
								<Dialog.Title>
									Squash "{session().name?.trim() || session().id.slice(0, 8)}"
									into {targetLabel()}?
								</Dialog.Title>
							</Dialog.Header>
							<box flexDirection="column">
								<text fg={theme.textPrimary}>
									The session will be summarized into {targetLabel()} and
									deleted.
								</text>
							</box>
							<Dialog.Footer>
								<KeymapHintBar borderless group="session-explorer" />
							</Dialog.Footer>
						</Dialog.Root>
					);
				}}
			</Show>
		</Dialog.Root>
	);
}
