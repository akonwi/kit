import { basename } from "node:path";
import { homedir } from "node:os";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { createStore } from "solid-js/store";
import type { AgentRuntime, RuntimeStatus } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import { matchCommands } from "../features/command-registry";
import { executeCommand, type SessionPickerItem } from "../features/commands";
import { createPaletteManager, type PaletteManager } from "./palette-manager";
import { emptySnapshot, type PaletteOption } from "./palette";

export type PanelState = {
	pending: boolean;
	title: string;
};

export type FooterStatusState = {
	cwd: string;
	model: string;
	thinkingLevel: string;
	contextPct: string;
};

export type SessionMeta = {
	sessionId: string;
	sessionName: string | undefined;
	sessionCwd: string;
	hasSession: boolean;
};

export type AppState = {
	messages: AgentMessage[];
	panel: PanelState;
	palette: import("./palette").PaletteSnapshot;
	footerStatus: FooterStatusState;
	sessionMeta: SessionMeta;
	debugEntry: string | null;
};

// ── Helpers ────────────────────────────────────────────────────────

function formatTimeAgo(date: Date): string {
	const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
	if (seconds < 60) return "just now";
	const minutes = Math.floor(seconds / 60);
	if (minutes < 60) return `${minutes}m ago`;
	const hours = Math.floor(minutes / 60);
	if (hours < 24) return `${hours}h ago`;
	const days = Math.floor(hours / 24);
	if (days < 30) return `${days}d ago`;
	return date.toLocaleDateString();
}

function formatCwd(rawCwd: string): string {
	const home = homedir();
	return rawCwd.startsWith(home) ? `~${rawCwd.slice(home.length)}` : rawCwd;
}

function deriveFooterStatus(
	runtime: AgentRuntime | null,
): Omit<FooterStatusState, "cwd"> {
	if (runtime) {
		const status = runtime.getStatus();
		return {
			model: status.model,
			thinkingLevel: status.thinkingLevel,
			contextPct: status.contextPct,
		};
	}
	return { model: "no-model", thinkingLevel: "off", contextPct: "–" };
}

function applyRuntimeStatus(
	current: FooterStatusState,
	status: RuntimeStatus,
): FooterStatusState {
	return {
		...current,
		model: status.model,
		thinkingLevel: status.thinkingLevel,
		contextPct: status.contextPct,
	};
}

function buildSessionMeta(session: LoadedSession | null): SessionMeta {
	if (session) {
		return {
			sessionId: session.sessionId,
			sessionName: session.sessionName,
			sessionCwd: session.cwd,
			hasSession: true,
		};
	}
	return {
		sessionId: "",
		sessionName: undefined,
		sessionCwd: process.cwd(),
		hasSession: false,
	};
}

// ── App state factory ──────────────────────────────────────────────

export function createAppState(
	_settings: LoadedSettings,
	session: LoadedSession | null,
	runtime: AgentRuntime | null,
) {
	const messages = runtime ? runtime.getMessages() : [];
	const footer = deriveFooterStatus(runtime);

	const [state, setState] = createStore<AppState>({
		messages,
		panel: { pending: false, title: "" },
		palette: emptySnapshot,
		footerStatus: { cwd: formatCwd(process.cwd()), ...footer },
		sessionMeta: buildSessionMeta(session),
		debugEntry: null,
	});

	const palette: PaletteManager = createPaletteManager(setState);

	// ── Runtime subscription ───────────────────────────────────────

	runtime?.subscribe((event) => {
		switch (event.type) {
			case "messages_changed":
				setState("messages", event.messages);
				break;
			case "status_changed":
				setState(
					"footerStatus",
					applyRuntimeStatus(state.footerStatus, event.status),
				);
				break;
			case "panel":
				setState("panel", event.panel);
				break;
			case "error":
				console.error(event);
				break;
		}
	});

	// ── Command palette (slash commands) ───────────────────────────

	let commandPaletteActive = false;

	function onComposerTextChange(text: string) {
		const trimmed = text.trimStart();

		if (trimmed.startsWith("/") && !trimmed.includes(" ")) {
			const matches = matchCommands(trimmed);
			if (matches.length > 0) {
				const options: PaletteOption[] = matches.map((c) => ({
					name: c.name,
					description: c.description,
					value: c,
					action: (ctx) => {
						commandPaletteActive = false;
						ctx.dismiss();
						handleSlashCommand(c.name);
					},
				}));
				if (commandPaletteActive) {
					palette.updateTopOptions(options);
				} else {
					palette.show({ options });
					commandPaletteActive = true;
				}
				return;
			}
		}

		if (commandPaletteActive) {
			commandPaletteActive = false;
			palette.pop();
		}
	}

	// ── Submit ─────────────────────────────────────────────────────

	async function onComposerSubmit(
		text: string,
	): Promise<{ composerText?: string }> {
		if (text.trimStart().startsWith("/")) {
			if (palette.visible) palette.clear();
			commandPaletteActive = false;
			handleSlashCommand(text);
			return {};
		}

		if (!runtime) {
			return { composerText: text };
		}

		try {
			await runtime.submitUserMessage(text);
			return {};
		} catch (error) {
			console.error(error);
			return { composerText: text };
		}
	}

	// ── Slash commands ─────────────────────────────────────────────

	async function handleSlashCommand(raw: string) {
		if (!runtime) return;

		try {
			const result = await executeCommand(raw, runtime);

			if (result.openModelPicker) {
				openModelPalette(result.openModelPicker.models);
			}
			if (result.openThinkingPicker) {
				openThinkingPalette(
					result.openThinkingPicker.levels,
					result.openThinkingPicker.current,
				);
			}
			if (result.openNameInput) {
				openNameInputPalette(result.openNameInput.currentName);
			}
			if (result.openSessionPicker) {
				openSessionSwitchPalette(result.openSessionPicker.sessions);
			}
			if (result.openSessionManage) {
				openSessionManagePalette(
					result.openSessionManage.sessions,
					result.openSessionManage.currentSessionId!,
				);
			}
		} catch (error) {
			console.error(error);
		}
	}

	// ── Thinking palette ─────────────────────────────────────────

	function openThinkingPalette(levels: string[], current: string) {
		const options: PaletteOption[] = levels.map((level) => ({
			name: level,
			description: level === current ? "(current)" : "",
			value: level,
			action: (ctx) => {
				runtime!.setThinkingLevel(
					level as import("@mariozechner/pi-agent-core").ThinkingLevel,
				);
				ctx.dismiss();
			},
		}));
		palette.show({ options });
	}

	// ── Name input palette ───────────────────────────────────────

	function openNameInputPalette(currentName: string) {
		palette.show({
			mode: "input",
			label: "Session name",
			inputValue: currentName,
			onSubmit: (value, ctx) => {
				if (value.trim()) {
					runtime!.setSessionName(value.trim());
					setState("sessionMeta", "sessionName", value.trim());
				}
				ctx.dismiss();
			},
		});
	}

	// ── Model palette ──────────────────────────────────────────────

	function openModelPalette(
		models: Array<{ id: string; name: string; provider: string }>,
	) {
		const options: PaletteOption[] = models.map((m) => ({
			name: m.name,
			description: m.provider,
			value: m,
			action: async (ctx) => {
				try {
					await runtime!.setModel(m.provider, m.id);
				} catch (error) {
					console.error(error);
				}
				ctx.dismiss();
			},
		}));
		palette.show({ options, filterable: true });
	}

	// ── Session switch palette ─────────────────────────────────────

	function openSessionSwitchPalette(sessions: SessionPickerItem[]) {
		const home = homedir();
		const options: PaletteOption[] = sessions.map((s) => {
			const label =
				s.name || s.firstMessage.slice(0, 60) || s.id.slice(0, 8);
			const cwd = s.cwd.startsWith(home)
				? `~${s.cwd.slice(home.length)}`
				: s.cwd;
			const dir = basename(cwd);
			const ago = formatTimeAgo(s.modified);
			return {
				name: label,
				description: `${dir}  ${ago}`,
				value: s,
				action: async (ctx) => {
					try {
						const ok = await runtime!.switchSession(s.path);
						if (ok) {
							const snap = runtime!.getSession();
							setState("sessionMeta", {
								sessionId: snap.sessionId,
								sessionName: snap.sessionName,
								sessionCwd: snap.cwd,
								hasSession: true,
							});
						}
					} catch (error) {
						console.error(error);
					}
					ctx.dismiss();
				},
			};
		});
		palette.show({ options, filterable: true });
	}

	// ── Session manage palette ─────────────────────────────────────

	function openSessionManagePalette(
		sessions: SessionPickerItem[],
		currentSessionId: string,
	) {
		let manageSessions = [...sessions];

		function buildOptions(): PaletteOption[] {
			const home = homedir();
			return manageSessions.map((s) => {
				const label =
					s.name || s.firstMessage.slice(0, 60) || s.id.slice(0, 8);
				const cwd = s.cwd.startsWith(home)
					? `~${s.cwd.slice(home.length)}`
					: s.cwd;
				const dir = basename(cwd);
				const ago = formatTimeAgo(s.modified);
				return {
					name: label,
					description: `${dir}  ${ago}`,
					value: s,
					action: () => {},
				};
			});
		}

		function refresh() {
			palette.pop();
			palette.show(
				{
					options: buildOptions(),
					filterable: true,
					hint: "Ctrl+R rename · Ctrl+D delete · Esc close",
				},
				{
					"ctrl+r": (option, _ctx) => {
						const session = option.value as SessionPickerItem;
						palette.show({
							mode: "input",
							label: "Rename session",
							inputValue: session.name || "",
							onSubmit: (value, inputCtx) => {
								try {
									runtime!.renameSession(session.path, value);
									session.name = value;
									if (session.id === currentSessionId) {
										setState("sessionMeta", "sessionName", value);
									}
								} catch (error) {
									console.error(error);
								}
								inputCtx.dismiss();
								refresh();
							},
						});
					},
					"ctrl+d": async (option, _ctx) => {
						const session = option.value as SessionPickerItem;
						if (session.id === currentSessionId) {
							return;
						}
						try {
							await runtime!.deleteSession(session.path);
							manageSessions = manageSessions.filter(
								(s) => s.id !== session.id,
							);
							refresh();
						} catch (error) {
							console.error(error);
						}
					},
				},
			);
		}

		refresh();
	}

	// ── Debug ──────────────────────────────────────────────────────

	return {
		state,
		palette,
		onComposerTextChange,
		onComposerSubmit,
	};
}
