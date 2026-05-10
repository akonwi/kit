import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	For,
	Show,
} from "solid-js";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { SessionSummary } from "../../session";
import { readSession, updateSession } from "../../session";
import { Dialog } from "../../shell/Dialog";
import { type Binding, HintBar } from "../../shell/HintBar";
import { theme } from "../../shell/theme";
import type { ToastInput } from "../../state/toasts";
import { formatTimeAgo } from "../commands/utils";
import {
	buildRelatedSessionTree,
	findSessionRowIndex,
	flattenSessionTree,
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

type Mode = "navigate" | "rename" | "confirmDelete" | "confirmSquash";

const NAVIGATE_BINDINGS: Binding[] = [
	{ key: "↑/↓", action: "move" },
	{ key: "PgUp/PgDn", action: "scroll" },
	{ key: "Enter", action: "switch" },
	{ key: "r", action: "rename" },
	{ key: "Esc", action: "close" },
];

const DELETE_BINDING: Binding = { key: "Ctrl+D", action: "delete" };

const RENAME_BINDINGS: Binding[] = [
	{ key: "Enter", action: "save" },
	{ key: "Esc", action: "cancel" },
];

const CONFIRM_BINDINGS: Binding[] = [
	{ key: "Enter", action: "confirm" },
	{ key: "Esc", action: "cancel" },
];

export function SessionExplorerModal(props: SessionExplorerModalProps) {
	const currentSessionId = () => props.runtime.getSession().id;
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [lastCenteredSessionId, setLastCenteredSessionId] = createSignal<
		string | null
	>(null);
	const [confirmSquashSession, setConfirmSquashSession] =
		createSignal<SessionSummary | null>(null);
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

	const mode = createMemo<Mode>(() => {
		if (confirmSquashSession()) return "confirmSquash";
		if (deleteSession()) return "confirmDelete";
		if (renameSession()) return "rename";
		return "navigate";
	});

	const rows = createMemo(() => {
		const list = sessions() ?? [];
		const root = buildRelatedSessionTree(list, currentSessionId());
		if (!root) return [];
		return flattenSessionTree(root, currentSessionId());
	});

	const currentSelectionIndex = createMemo(() =>
		findSessionRowIndex(rows(), currentSessionId()),
	);
	const selectedRow = createMemo(() => rows()[selectedIndex()] ?? null);
	const selectedSessionId = createMemo(() => selectedRow()?.session.id ?? null);
	const selectedSessionCanSquash = createMemo(() =>
		Boolean(selectedRow()?.isCurrent && selectedRow()?.session.parentSessionId),
	);
	const selectedSessionCanDelete = createMemo(() =>
		Boolean(selectedRow() && !selectedRow()?.isCurrent),
	);
	const bindings = createMemo<Binding[]>(() => {
		switch (mode()) {
			case "rename":
				return RENAME_BINDINGS;
			case "confirmDelete":
			case "confirmSquash":
				return CONFIRM_BINDINGS;
			default:
				return [
					...NAVIGATE_BINDINGS,
					...(selectedSessionCanDelete() ? [DELETE_BINDING] : []),
					...(selectedSessionCanSquash()
						? [{ key: "s", action: "squash" }]
						: []),
				];
		}
	});
	const visibleSlice = createMemo(() => {
		const allRows = rows();
		if (allRows.length <= MAX_VISIBLE_ROWS) {
			return { rows: allRows, offset: 0 };
		}

		let offset = selectedIndex() - Math.floor(MAX_VISIBLE_ROWS / 2);
		offset = Math.max(0, Math.min(offset, allRows.length - MAX_VISIBLE_ROWS));
		return {
			rows: allRows.slice(offset, offset + MAX_VISIBLE_ROWS),
			offset,
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
				lines: ["Switch to another session before deleting this one."],
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
				lines: ["Session could not be found."],
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
				lines: [String(error)],
				variant: "error",
			});
		}
	}

	useKeyboard((e: KeyEvent) => {
		if (mode() === "confirmSquash") {
			if (e.name === "escape" || (e.ctrl && e.name === "c")) {
				e.preventDefault();
				setConfirmSquashSession(null);
				return;
			}
			if (e.name === "return" || e.name === "enter") {
				e.preventDefault();
				setConfirmSquashSession(null);
				props.onClose();
				void props.runtime.mergeUp().catch((error) => {
					props.toast({
						title: "Squash failed",
						lines: [String(error)],
						variant: "error",
					});
				});
				return;
			}
			return;
		}

		if (mode() === "confirmDelete") {
			if (e.name === "escape" || (e.ctrl && e.name === "c")) {
				e.preventDefault();
				setDeleteSession(null);
				return;
			}
			if (e.name === "return" || e.name === "enter") {
				e.preventDefault();
				void handleDeleteConfirm();
				return;
			}
			return;
		}

		if (mode() === "rename") {
			if (e.name === "escape" || (e.ctrl && e.name === "c")) {
				e.preventDefault();
				setRenameSession(null);
				return;
			}
			if (e.name === "return" || e.name === "enter") {
				e.preventDefault();
				void handleRenameSubmit();
				return;
			}
			return;
		}

		if (e.name === "escape" || (e.ctrl && e.name === "c")) {
			e.preventDefault();
			props.onClose();
			return;
		}

		if (e.name === "return" || e.name === "enter") {
			e.preventDefault();
			props.onSelect(selectedSessionId());
			return;
		}

		if (e.name === "up" || e.name === "k") {
			e.preventDefault();
			setSelectedIndex((index) => Math.max(0, index - 1));
			return;
		}
		if (e.name === "down" || e.name === "j") {
			e.preventDefault();
			setSelectedIndex((index) => Math.min(rows().length - 1, index + 1));
			return;
		}
		if (e.name === "pageup") {
			e.preventDefault();
			setSelectedIndex((index) => Math.max(0, index - MAX_VISIBLE_ROWS));
			return;
		}
		if (e.name === "pagedown") {
			e.preventDefault();
			setSelectedIndex((index) =>
				Math.min(rows().length - 1, index + MAX_VISIBLE_ROWS),
			);
			return;
		}
		if (e.name === "r") {
			e.preventDefault();
			beginRename();
			return;
		}
		if (e.ctrl && e.name === "d") {
			e.preventDefault();
			if (selectedSessionCanDelete()) beginDelete();
			return;
		}
		if (e.name === "s" && selectedSessionCanSquash()) {
			e.preventDefault();
			const session = selectedRow()?.session;
			if (!session) return;
			setConfirmSquashSession(session);
		}
	});

	const treeFooter = createMemo(() => {
		const allRows = rows();
		if (allRows.length <= MAX_VISIBLE_ROWS) return null;
		const start = visibleSlice().offset + 1;
		const end = visibleSlice().offset + visibleSlice().rows.length;
		return `Showing ${start}-${end} of ${allRows.length}`;
	});

	return (
		<Dialog.Root width="85%" maxWidth={120} minWidth={64} height="80%">
			<Dialog.Header>
				<Dialog.Title>Session Explorer</Dialog.Title>
				<Dialog.Meta>{rows().length} related</Dialog.Meta>
			</Dialog.Header>

			<Show
				when={!sessions.loading}
				fallback={<text fg={theme.textMuted}>Loading sessions…</text>}
			>
				<box flexGrow={1} flexDirection="column">
					<Show
						when={rows().length > 0}
						fallback={
							<text fg={theme.textMuted}>No related sessions found.</text>
						}
					>
						<scrollbox
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
								<For each={visibleSlice().rows}>
									{(row, idx) => {
										const absoluteIndex = () => visibleSlice().offset + idx();
										const focused = () => absoluteIndex() === selectedIndex();
										const label = () => getSessionTreeTitle(row);
										const treePrefix = () => formatSessionTreePrefix(row);
										const meta = () =>
											`${row.session.id.slice(0, 8)} · ${formatTimeAgo(new Date(row.session.updatedAt))}`;
										const labelColor = () =>
											row.isCurrent ? theme.borderAccent : theme.textPrimary;
										return (
											<box
												paddingX={1}
												flexDirection="row"
												backgroundColor={
													focused() ? theme.bgMuted : theme.bgTransparent
												}
											>
												<text fg={labelColor()}>
													{treePrefix()}
													{label()}
												</text>
												<text fg={theme.textMuted}>{` · ${meta()}`}</text>
											</box>
										);
									}}
								</For>
							</box>
						</scrollbox>
						<Show when={treeFooter()}>
							<text fg={theme.textMuted}>{treeFooter()}</text>
						</Show>
					</Show>
				</box>
			</Show>

			<Dialog.Footer>
				<HintBar bindings={bindings()} />
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
							<HintBar bindings={RENAME_BINDINGS} />
						</Dialog.Footer>
					</Dialog.Root>
				)}
			</Show>

			<Show when={deleteSession()}>
				{(session) => (
					<Dialog.Root maxWidth={80}>
						<Dialog.Header>
							<Dialog.Title>
								Delete "{session().name?.trim() || session().id.slice(0, 8)}"?
							</Dialog.Title>
						</Dialog.Header>
						<text fg={theme.errorText}>
							This permanently removes the saved session.
						</text>
						<Dialog.Footer>
							<HintBar bindings={CONFIRM_BINDINGS} />
						</Dialog.Footer>
					</Dialog.Root>
				)}
			</Show>

			<Show when={confirmSquashSession()}>
				{(session) => (
					<Dialog.Root maxWidth={80}>
						<Dialog.Header>
							<Dialog.Title>
								Squash "{session().name?.trim() || session().id.slice(0, 8)}"
								into its parent?
							</Dialog.Title>
						</Dialog.Header>
						<box flexDirection="column" gap={0} paddingLeft={1}>
							<text fg={theme.textMuted}>
								• append a summary into the parent
							</text>
							<text fg={theme.textMuted}>• switch back to the parent</text>
							<text fg={theme.textMuted}>• delete the child session</text>
						</box>
						<Dialog.Footer>
							<HintBar bindings={CONFIRM_BINDINGS} />
						</Dialog.Footer>
					</Dialog.Root>
				)}
			</Show>
		</Dialog.Root>
	);
}
