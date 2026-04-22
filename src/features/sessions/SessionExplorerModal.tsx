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
import { theme } from "../../shell/theme";
import { formatTimeAgo } from "../commands/utils";
import {
	buildRelatedSessionTree,
	findSessionRowIndex,
	flattenSessionTree,
	formatSessionTreeLabel,
} from "./tree";

export type SessionExplorerModalProps = {
	runtime: AgentRuntime;
	onClose: () => void;
	onSelect: (sessionId: string | null) => void;
};

const MAX_VISIBLE_ROWS = 18;

export function SessionExplorerModal(props: SessionExplorerModalProps) {
	const currentSessionId = () => props.runtime.getSession().id;
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [lastCenteredSessionId, setLastCenteredSessionId] = createSignal<
		string | null
	>(null);
	const [sessions] = createResource(async () => props.runtime.listAllSessions());

	const rows = createMemo(() => {
		const list = sessions() ?? [];
		const root = buildRelatedSessionTree(list, currentSessionId());
		if (!root) return [];
		return flattenSessionTree(root, currentSessionId());
	});

	const currentSelectionIndex = createMemo(() =>
		findSessionRowIndex(rows(), currentSessionId()),
	);
	const selectedSessionId = createMemo(
		() => rows()[selectedIndex()]?.session.id ?? null,
	);
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

		setSelectedIndex((index) => Math.max(0, Math.min(index, allRows.length - 1)));
	});

	useKeyboard((e: KeyEvent) => {
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
				<text fg={theme.textPrimary}>Session Explorer</text>

				<Show when={!sessions.loading} fallback={<text fg={theme.textMuted}>Loading sessions…</text>}>
					<box
						flexGrow={1}
						border
						borderColor={theme.borderDefault}
						padding={1}
						flexDirection="column"
					>
						<text fg={theme.textPrimary}>Related sessions</text>
						<Show
							when={rows().length > 0}
							fallback={<text fg={theme.textMuted}>No related sessions found.</text>}
						>
							<box flexDirection="column" flexGrow={1}>
								<For each={visibleSlice().rows}>
									{(row, idx) => {
										const absoluteIndex = () => visibleSlice().offset + idx();
										const focused = () => absoluteIndex() === selectedIndex();
										const label = () => formatSessionTreeLabel(row);
										const meta = () =>
											`${row.session.id.slice(0, 8)} · ${formatTimeAgo(new Date(row.session.updatedAt))}`;
										const labelColor = () =>
											row.isCurrent
												? theme.borderAccent
												: focused()
													? theme.pickerFocusedText
													: theme.textPrimary;
										return (
											<box
												backgroundColor={
													focused() ? theme.pickerFocusedBg : theme.bgTransparent
												}
											>
												<text fg={labelColor()}>
													{row.isCurrent ? "• " : "  "}
													{label()}
												</text>
												<text
													fg={
														focused()
															? theme.pickerFocusedText
															: theme.textMuted
													}
												>
													 {meta()}
												</text>
											</box>
										);
									}}
								</For>
								<Show when={treeFooter()}>
									<text fg={theme.textMuted}>{treeFooter()}</text>
								</Show>
							</box>
						</Show>
					</box>
				</Show>

				<text fg={theme.textMuted}>
					↑/↓ move · PgUp/PgDn scroll · Enter switch · Esc close
				</text>
			</box>
		</box>
	);
}
