import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import { createMemo, createResource, createSignal, For, Show } from "solid-js";
import type { AgentRuntime } from "../../runtime/agent-runtime";
import type { SessionSummary } from "../../session";
import { formatTimeAgo } from "../commands/utils";
import {
	buildRelatedSessionTree,
	findSessionRowIndex,
	flattenSessionTree,
	formatSessionTreeLabel,
} from "./tree";
import { theme } from "../../shell/theme";

export type SessionExplorerModalProps = {
	runtime: AgentRuntime;
	onClose: () => void;
	onSelect: (sessionId: string | null) => void;
};

export function SessionExplorerModal(props: SessionExplorerModalProps) {
	const currentSessionId = () => props.runtime.getSession().id;
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [sessions] = createResource(async () => props.runtime.listAllSessions());

	const rows = createMemo(() => {
		const list = sessions() ?? [];
		const root = buildRelatedSessionTree(list, currentSessionId());
		if (!root) return [];
		return flattenSessionTree(root, currentSessionId());
	});

	const selectedRow = createMemo(() => rows()[selectedIndex()]);
	const selectedSession = createMemo(() => selectedRow()?.session ?? null);
	const currentSelectionIndex = createMemo(() =>
		findSessionRowIndex(rows(), currentSessionId()),
	);

	createMemo(() => {
		const currentIndex = currentSelectionIndex();
		if (currentIndex >= 0 && selectedIndex() >= rows().length) {
			setSelectedIndex(currentIndex);
		}
		if (selectedIndex() < 0 && rows().length > 0) {
			setSelectedIndex(0);
		}
	});

	useKeyboard((e: KeyEvent) => {
		if (e.name === "escape" || (e.ctrl && e.name === "c")) {
			e.preventDefault();
			props.onClose();
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
		if (e.name === "return" || e.name === "enter") {
			e.preventDefault();
			props.onSelect(selectedSession()?.id ?? null);
		}
	});

	function sessionRole(session: SessionSummary | null): string {
		if (!session) return "";
		if (session.id === currentSessionId()) return "current";
		if (session.parentSessionId === currentSessionId()) return "child";
		return session.parentSessionId ? "related" : "root";
	}

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
				<text fg={theme.textMuted}>
					Current: {props.runtime.getSession().name || props.runtime.getSession().id.slice(0, 8)}
				</text>

				<Show when={!sessions.loading} fallback={<text fg={theme.textMuted}>Loading sessions…</text>}>
					<box flexGrow={1} flexDirection="row" gap={1}>
						<box
							width="55%"
							border
							borderColor={theme.borderDefault}
							padding={1}
							flexDirection="column"
						>
							<text fg={theme.textPrimary}>Related sessions</text>
							<Show when={rows().length > 0} fallback={<text fg={theme.textMuted}>No related sessions found.</text>}>
								<For each={rows()}>
									{(row, idx) => {
										const focused = () => idx() === selectedIndex();
										const label = () => formatSessionTreeLabel(row);
										const meta = () => `${row.session.id.slice(0, 8)} · ${formatTimeAgo(new Date(row.session.updatedAt))}`;
										return (
											<box backgroundColor={focused() ? theme.pickerFocusedBg : theme.bgTransparent}>
												<text fg={focused() ? theme.pickerFocusedText : row.isCurrent ? theme.borderAccent : theme.textPrimary}>
													{focused() ? "› " : "  "}{label()}
												</text>
												<text fg={focused() ? theme.pickerFocusedText : theme.textMuted}>
													 {meta()}
												</text>
											</box>
										);
									}}
								</For>
							</Show>
						</box>

						<box
							flexGrow={1}
							border
							borderColor={theme.borderDefault}
							padding={1}
							flexDirection="column"
							gap={1}
						>
							<text fg={theme.textPrimary}>Details</text>
							<Show when={selectedSession()} fallback={<text fg={theme.textMuted}>Select a session.</text>}>
								{text => (
									<>
										<text fg={theme.textPrimary}>{text().name || text().firstMessage || text().id.slice(0, 8)}</text>
										<text fg={theme.textMuted}>ID: {text().id}</text>
										<text fg={theme.textMuted}>Role: {sessionRole(text())}</text>
										<text fg={theme.textMuted}>Parent: {text().parentSessionId ?? "(none)"}</text>
										<text fg={theme.textMuted}>CWD: {text().cwd}</text>
										<text fg={theme.textMuted}>Updated: {new Date(text().updatedAt).toLocaleString()}</text>
										<text fg={theme.textMuted}>Messages: {text().messageCount}</text>
										<Show when={text().firstMessage}>
											<box flexDirection="column">
												<text fg={theme.textPrimary}>First message</text>
												<text fg={theme.textSecondary}>{text().firstMessage}</text>
											</box>
										</Show>
									</>
								)}
							</Show>
						</box>
					</box>
				</Show>

				<text fg={theme.textMuted}>↑/↓ move · Enter switch · Esc close</text>
			</box>
		</box>
	);
}
