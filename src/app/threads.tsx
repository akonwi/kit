/**
 * `kit threads` — standalone TUI session picker.
 *
 * Shows sessions for the current cwd and lets the user pick one to open,
 * delete, or rename.
 */

import type { KeyEvent } from "@opentui/core";
import { createCliRenderer } from "@opentui/core";
import { render, useKeyboard } from "@opentui/solid";
import { createSignal, For, Show } from "solid-js";
import { formatSessionOption, formatTimeAgo } from "../features/commands/utils";
import type { SessionSummary } from "../session";
import {
	deleteSession,
	listSessionsForCwd,
	readSession,
	updateSession,
} from "../session";
import { loadSettings } from "../settings";
import { type Binding, HintBar } from "../shell/HintBar";
import { resolveAndApplyTheme, theme } from "../shell/theme";

type Mode = "navigate" | "rename" | "confirmDelete";

const MODE_BINDINGS: { [key in Mode]: Binding[] } = {
	rename: [
		{ key: "Enter", action: "save" },
		{ key: "Esc", action: "cancel" },
	],
	confirmDelete: [],
	navigate: [
		{ key: "↑/↓", action: "navigate" },
		{ key: "Enter", action: "open" },
		{ key: "r", action: "rename" },
		{ key: "Ctrl+D", action: "delete" },
		{ key: "Esc", action: "quit" },
	],
};

function ThreadPicker(props: {
	initialSessions: SessionSummary[];
	onSelect: (id: string) => void;
	onCancel: () => void;
	onEmpty: () => void;
}) {
	const [sessions, setSessions] = createSignal(props.initialSessions);
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [mode, setMode] = createSignal<Mode>("navigate");
	const [renameText, setRenameText] = createSignal("");

	let renameRef:
		| { plainText: string; setText: (v: string) => void }
		| undefined;

	const home = process.env.HOME || process.env.USERPROFILE || "";

	const widths = () =>
		sessions().reduce(
			(acc, session) => {
				const cwd = session.cwd.startsWith(home)
					? `~${session.cwd.slice(home.length)}`
					: session.cwd;
				const updatedAt = formatTimeAgo(new Date(session.updatedAt));
				return {
					cwd: Math.max(acc.cwd, cwd.length),
					updatedAt: Math.max(acc.updatedAt, updatedAt.length),
				};
			},
			{ cwd: 0, updatedAt: 0 },
		);

	const options = () =>
		sessions().map((session) => {
			const { label, description } = formatSessionOption(session, widths());
			return { label, description, id: session.id, name: session.name };
		});

	function clampIndex() {
		const max = sessions().length - 1;
		if (selectedIndex() > max) setSelectedIndex(Math.max(0, max));
	}

	async function handleDelete() {
		const opt = options()[selectedIndex()];
		if (!opt) return;
		await deleteSession(opt.id);
		setSessions((prev) => prev.filter((s) => s.id !== opt.id));
		clampIndex();
		setMode("navigate");
		if (sessions().length === 0) props.onEmpty();
	}

	async function handleRename() {
		const opt = options()[selectedIndex()];
		if (!opt) return;
		const newName = renameText().trim();
		if (!newName) {
			setMode("navigate");
			return;
		}
		const session = await readSession(opt.id);
		if (session) {
			await updateSession(session, { name: newName });
			setSessions((prev) =>
				prev.map((s) => (s.id === opt.id ? { ...s, name: newName } : s)),
			);
		}
		setMode("navigate");
	}

	useKeyboard((e: KeyEvent) => {
		if (mode() === "confirmDelete") {
			if (e.name === "return" || e.name === "y") {
				e.preventDefault();
				void handleDelete();
			} else {
				e.preventDefault();
				setMode("navigate");
			}
			return;
		}

		if (mode() === "rename") {
			if (e.name === "escape") {
				e.preventDefault();
				setMode("navigate");
			} else if (e.name === "return") {
				e.preventDefault();
				void handleRename();
			}
			return;
		}

		// Navigate mode
		if (e.name === "escape" || (e.ctrl && e.name === "c")) {
			e.preventDefault();
			props.onCancel();
			return;
		}
		if (e.name === "up" || e.name === "k") {
			e.preventDefault();
			setSelectedIndex((i) => Math.max(0, i - 1));
			return;
		}
		if (e.name === "down" || e.name === "j") {
			e.preventDefault();
			setSelectedIndex((i) => Math.min(sessions().length - 1, i + 1));
			return;
		}
		if (e.name === "return") {
			e.preventDefault();
			const opt = options()[selectedIndex()];
			if (opt) props.onSelect(opt.id);
			return;
		}
		if (e.ctrl && e.name === "d") {
			e.preventDefault();
			if (sessions().length > 0) setMode("confirmDelete");
			return;
		}
		if (e.name === "r") {
			e.preventDefault();
			const opt = options()[selectedIndex()];
			if (opt) {
				setRenameText(opt.name || "");
				setMode("rename");
				// Set textarea content after it mounts
				setTimeout(() => renameRef?.setText(opt.name || ""), 0);
			}
		}
	});

	const currentOpt = () => options()[selectedIndex()];

	return (
		<box
			flexDirection="column"
			width="100%"
			height="100%"
			backgroundColor={theme.bg}
		>
			<box paddingX={1} paddingY={1}>
				<text fg={theme.textPrimary}>
					<b>Threads for {process.cwd()}</b>
				</text>
			</box>
			<box flexDirection="column" flexGrow={1} paddingX={1}>
				<For each={options()}>
					{(opt, idx) => {
						const focused = () => idx() === selectedIndex();
						return (
							<box
								backgroundColor={
									focused() ? theme.pickerFocusedBg : theme.bgTransparent
								}
							>
								<text
									fg={focused() ? theme.pickerFocusedText : theme.textPrimary}
								>
									{focused() ? "› " : "  "}
									{opt.label} {opt.description}
								</text>
							</box>
						);
					}}
				</For>
			</box>

			{/* Rename input */}
			<Show when={mode() === "rename"}>
				<box paddingX={1} flexDirection="row" gap={1}>
					<text fg={theme.textPrimary}>Rename:</text>
					<textarea
						ref={(el) => {
							renameRef = el as typeof renameRef;
						}}
						minHeight={1}
						maxHeight={1}
						placeholder="Enter new name..."
						placeholderColor={theme.textPlaceholder}
						backgroundColor={theme.bg}
						focusedBackgroundColor={theme.bg}
						textColor={theme.textPrimary}
						focusedTextColor={theme.textPrimary}
						cursorColor={theme.cursor}
						showCursor
						focused={mode() === "rename"}
						onContentChange={() => setRenameText(renameRef?.plainText ?? "")}
					/>
				</box>
			</Show>

			{/* Delete confirmation */}
			<Show when={mode() === "confirmDelete"}>
				<box paddingX={1}>
					<text fg={theme.errorText}>
						Delete "{currentOpt()?.label}"? Enter to confirm, any other key to
						cancel
					</text>
				</box>
			</Show>

			{/* Hints */}
			<HintBar bindings={MODE_BINDINGS[mode()]} />
		</box>
	);
}

function EmptyState(props: { onCancel: () => void }) {
	useKeyboard((e: KeyEvent) => {
		if (
			e.name === "escape" ||
			(e.ctrl && e.name === "c") ||
			e.name === "return"
		) {
			e.preventDefault();
			props.onCancel();
		}
	});

	return (
		<box
			flexDirection="column"
			width="100%"
			height="100%"
			justifyContent="center"
			alignItems="center"
			backgroundColor={theme.bg}
		>
			<text fg={theme.textPrimary}>No threads found for this directory.</text>
			<text fg={theme.textMuted}>Press any key to exit.</text>
		</box>
	);
}

export async function showThreadPicker(): Promise<string | null> {
	const sessions = await listSessionsForCwd(process.cwd());
	const footerHeight =
		sessions.length === 0 ? 5 : Math.max(10, Math.min(sessions.length + 4, 20));
	const renderer = await createCliRenderer({
		exitOnCtrlC: sessions.length === 0,
		screenMode: "split-footer",
		footerHeight,
	});
	const settings = await loadSettings();
	await resolveAndApplyTheme(settings.settings.theme ?? "system", renderer);

	if (sessions.length === 0) {
		return new Promise<null>((resolve) => {
			render(
				() => (
					<EmptyState
						onCancel={() => {
							renderer.destroy();
							resolve(null);
						}}
					/>
				),
				renderer,
			);
		});
	}

	return new Promise<string | null>((resolve) => {
		render(
			() => (
				<ThreadPicker
					initialSessions={sessions}
					onSelect={(id) => {
						renderer.destroy();
						resolve(id);
					}}
					onCancel={() => {
						renderer.destroy();
						resolve(null);
					}}
					onEmpty={() => {
						renderer.destroy();
						resolve(null);
					}}
				/>
			),
			renderer,
		);
	});
}
