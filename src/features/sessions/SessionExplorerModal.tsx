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
import { type Binding, HintBar } from "../../shell/HintBar";
import { theme } from "../../shell/theme";
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
	onClose: () => void;
	onSelect: (sessionId: string | null) => void;
};

const MAX_VISIBLE_ROWS = 18;

const BASE_BINDINGS: Binding[] = [
	{ key: "↑/↓", action: "move" },
	{ key: "PgUp/PgDn", action: "scroll" },
	{ key: "Enter", action: "switch" },
	{ key: "Esc", action: "close" },
];

const CONFIRM_SQUASH_BINDINGS: Binding[] = [
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
	const [sessions] = createResource(async () =>
		props.runtime.listAllSessions(),
	);

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
	const bindings = createMemo<Binding[]>(() => {
		if (confirmSquashSession()) return CONFIRM_SQUASH_BINDINGS;
		return [
			...BASE_BINDINGS,
			...(selectedSessionCanSquash() ? [{ key: "s", action: "squash" }] : []),
		];
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

	useKeyboard((e: KeyEvent) => {
		if (confirmSquashSession()) {
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
					props.runtime.emitError("Squash failed", [String(error)]);
				});
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
		<box
			position="absolute"
			left={0}
			top={0}
			width="100%"
			height="100%"
			justifyContent="center"
			alignItems="center"
			zIndex={1100}
			backgroundColor={theme.modalBackdrop}
		>
			<box
				width="85%"
				maxWidth={120}
				minWidth={64}
				height="80%"
				border
				borderStyle="double"
				borderColor={theme.borderFocused}
				backgroundColor={theme.bgSurface}
				padding={1}
				flexDirection="column"
				gap={1}
			>
				<box flexShrink={0} flexDirection="row" justifyContent="space-between">
					<text fg={theme.textPrimary}>Session Explorer</text>
					<text fg={theme.textMuted}>{rows().length} related</text>
				</box>

				<Show
					when={!sessions.loading}
					fallback={<text fg={theme.textMuted}>Loading sessions…</text>}
				>
					<box
						flexGrow={1}
						border
						borderColor={theme.borderDefault}
						padding={1}
						flexDirection="column"
					>
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
														{row.isCurrent ? "• " : "  "}
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

				<HintBar bindings={bindings()} />

				<Show when={confirmSquashSession()}>
					{(session) => (
						<box
							position="absolute"
							left={0}
							top={0}
							width="100%"
							height="100%"
							justifyContent="center"
							alignItems="center"
							backgroundColor={theme.modalBackdrop}
						>
							<box
								width="70%"
								maxWidth={80}
								minWidth={48}
								border
								borderStyle="double"
								borderColor={theme.borderFocused}
								backgroundColor={theme.bgSurface}
								padding={1}
								flexDirection="column"
								gap={1}
							>
								<text fg={theme.textPrimary}>
									Squash "{session().name?.trim() || session().id.slice(0, 8)}"
									into its parent?
								</text>
								<box flexDirection="column" gap={0} paddingLeft={1}>
									<text fg={theme.textMuted}>
										• append a summary into the parent
									</text>
									<text fg={theme.textMuted}>• switch back to the parent</text>
									<text fg={theme.textMuted}>• delete the child session</text>
								</box>
								<text fg={theme.textMuted}>
									Press Enter to confirm or Esc to cancel.
								</text>
							</box>
						</box>
					)}
				</Show>
			</box>
		</box>
	);
}
