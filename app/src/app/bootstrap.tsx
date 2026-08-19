import path from "node:path";
import { parseArgs } from "node:util";
import {
	CliRenderEvents,
	ConsolePosition,
	createCliRenderer,
	getTreeSitterClient,
} from "@opentui/core";
import { KeymapProvider } from "@opentui/keymap/solid";
import { render } from "@opentui/solid";
import { ringLocalBell } from "../features/notifications/notifications";
import { createKitKeymap } from "../keymap/setup";
import { safeProcessCwd } from "../process-cwd";
import { getInstalledRuntimeDir } from "../runtime/runtime-dir";
import type { Session } from "../session";
import { loadSettings } from "../settings";
import { copyToClipboard } from "../shell/clipboard";
import { initTemplates } from "../shell/templates";
import { setTerminalProgress } from "../shell/terminal-progress";
import {
	initTerminalTitle,
	setTerminalTitleTurnActive,
	updateTerminalTitle,
} from "../shell/terminal-title";
import { getCurrentThemeConfig, resolveAndApplyTheme } from "../shell/theme";
import { App } from "./App";
import { startShutdownWatchdog } from "./shutdown-watchdog";

type BootstrapOpts = {
	sessionId?: string;
	newSession?: boolean;
	noSession?: boolean;
	/**
	 * Experimental: host the OpenTUI application against custom terminal
	 * streams instead of the process TTY (browser-TUI bridge).
	 */
	terminal?: BootstrapTerminal;
};

export type BootstrapTerminal = {
	stdin: NodeJS.ReadStream;
	stdout: NodeJS.WriteStream;
	width: number;
	height: number;
	onRendererReady?: (
		renderer: Awaited<ReturnType<typeof createCliRenderer>>,
	) => void;
	copyText?: (text: string) => Promise<void>;
	notify?: (message: string, title?: string) => boolean;
	bell?: (isError: boolean) => void;
};

async function loadSession(opts?: BootstrapOpts): Promise<Session> {
	// If a session ID is provided directly (e.g. from `kit threads`), use it
	const sessionArg =
		opts?.sessionId ??
		(parseArgs({
			args: process.argv.slice(2),
			options: { session: { type: "string", short: "s" } },
			strict: false,
		}).values.session as string | undefined);

	if (sessionArg) {
		const { findSessionById, readSession } = await import("../session");
		const session =
			(await findSessionById(sessionArg)) ?? (await readSession(sessionArg));
		if (!session) {
			console.error(`Session not found: ${sessionArg}`);
			process.exit(1);
		}
		return session;
	}

	const cwd = safeProcessCwd();
	if (opts?.noSession || opts?.newSession) {
		const { createSession } = await import("../session");
		return createSession(cwd);
	}

	const { openRecentSession } = await import("../session");
	return openRecentSession(cwd);
}

export async function bootstrap(opts?: BootstrapOpts): Promise<void> {
	// Legacy support for older wrapper-based launches. The compiled binary should
	// run directly from the user's current working directory and not need this.
	const userCwd = process.env.KIT_USER_CWD;
	if (userCwd && userCwd !== safeProcessCwd()) {
		process.chdir(userCwd);
	}

	// Initialize tree-sitter and register additional filetype aliases.
	const installedRuntimeDir = getInstalledRuntimeDir();
	if (installedRuntimeDir) {
		process.env.OTUI_TREE_SITTER_WORKER_PATH = path.join(
			installedRuntimeDir,
			"parser.worker.js",
		);
	}

	const treeSitter = getTreeSitterClient();
	await treeSitter.initialize();

	const coreAssets = installedRuntimeDir
		? path.join(installedRuntimeDir, "assets")
		: path.resolve(
				import.meta.dirname,
				"../../node_modules/@opentui/core/assets",
			);
	treeSitter.addFiletypeParser({
		filetype: "tsx",
		wasm: path.join(coreAssets, "typescript/tree-sitter-typescript.wasm"),
		queries: {
			highlights: [path.join(coreAssets, "typescript/highlights.scm")],
		},
	});
	treeSitter.addFiletypeParser({
		filetype: "jsx",
		wasm: path.join(coreAssets, "javascript/tree-sitter-javascript.wasm"),
		queries: {
			highlights: [path.join(coreAssets, "javascript/highlights.scm")],
		},
	});

	const kitGrammars = installedRuntimeDir
		? path.join(installedRuntimeDir, "grammars")
		: path.resolve(import.meta.dirname, "../../grammars");
	for (const grammar of [
		{ filetype: "ard", wasm: "tree-sitter-ard.wasm" },
		{ filetype: "bash", wasm: "tree-sitter-bash.wasm" },
		{ filetype: "css", wasm: "tree-sitter-css.wasm" },
		{ filetype: "go", wasm: "tree-sitter-go.wasm" },
		{ filetype: "html", wasm: "tree-sitter-html.wasm" },
		{ filetype: "json", wasm: "tree-sitter-json.wasm" },
		{ filetype: "python", wasm: "tree-sitter-python.wasm" },
		{ filetype: "ruby", wasm: "tree-sitter-ruby.wasm", aliases: ["rake"] },
		{ filetype: "rust", wasm: "tree-sitter-rust.wasm" },
		{ filetype: "toml", wasm: "tree-sitter-toml.wasm" },
		{ filetype: "yaml", wasm: "tree-sitter-yaml.wasm" },
	]) {
		treeSitter.addFiletypeParser({
			filetype: grammar.filetype,
			aliases: grammar.aliases,
			wasm: path.join(kitGrammars, grammar.filetype, grammar.wasm),
			queries: {
				highlights: [
					path.join(kitGrammars, grammar.filetype, "highlights.scm"),
				],
			},
		});
	}

	const settings = await loadSettings();
	const session = await loadSession(opts);

	let disposeApp: (() => void | Promise<void>) | null = null;
	let resolveAlive: (() => void) | null = null;
	let quitStarted = false;
	// A custom-terminal host owns the surrounding process and any additional
	// resources (for example, a web server). Only standalone TTY bootstrap may
	// arm the fallback process watchdog directly.
	const usesProcessStdio = opts?.terminal === undefined;

	const renderer = await createCliRenderer({
		...(opts?.terminal
			? {
					stdin: opts.terminal.stdin,
					stdout: opts.terminal.stdout,
					width: opts.terminal.width,
					height: opts.terminal.height,
					// Browser input is normalized by Kit's transport adapter. Keep a
					// deterministic legacy protocol until that adapter tracks Kitty flags.
					useKittyKeyboard: null,
				}
			: {}),
		exitOnCtrlC: false,
		exitSignals: [
			"SIGTERM",
			"SIGQUIT",
			"SIGABRT",
			"SIGHUP",
			"SIGBREAK",
			"SIGPIPE",
			"SIGBUS",
			"SIGFPE",
		],
		onDestroy: () => {
			quitStarted = true;
			const cleanup = disposeApp;
			disposeApp = null;
			void Promise.resolve()
				.then(() => cleanup?.())
				.catch((error) => {
					console.error(
						`[kit] shutdown cleanup failed: ${error instanceof Error ? error.message : String(error)}`,
					);
				})
				.finally(() => {
					resolveAlive?.();
					resolveAlive = null;
					if (usesProcessStdio) {
						startShutdownWatchdog(
							typeof process.exitCode === "number" ? process.exitCode : 0,
						);
					}
				});
		},
		consoleOptions: {
			position: ConsolePosition.TOP,
			sizePercent: 30,
		},
	});

	try {
		// Dev console toggle is opt-in — only enable when DEBUG is set,
		// otherwise Ctrl+D is left free for other key handlers.
		if (process.env.DEBUG) {
			renderer.keyInput.on("keypress", (key) => {
				if (key.ctrl && key.name === "d") {
					renderer.console.toggle();
				}
			});
		}

		const keymap = createKitKeymap(renderer);

		// Resolve theme before rendering — "system" theme needs the renderer for palette detection
		const themeName = settings.settings.theme ?? "system";
		await resolveAndApplyTheme(themeName, renderer);

		// Re-resolve the theme when the terminal reports a color scheme change
		// (e.g. the user switches between light and dark mode in their OS).
		renderer.on(CliRenderEvents.THEME_MODE, () => {
			const currentTheme = getCurrentThemeConfig().name;
			void resolveAndApplyTheme(currentTheme, undefined, {
				invalidateSystemCache: true,
			});
		});

		initTerminalTitle((title) => renderer.setTerminalTitle(title));
		updateTerminalTitle(session.name, session.cwd);
		initTemplates(session.cwd);

		// Keep the process alive until the renderer is destroyed.
		// In compiled binaries, the async bootstrap() returning would let
		// the event loop drain and the process exit prematurely.
		function quitAndDestroy(): void {
			if (quitStarted) return;
			quitStarted = true;
			renderer.destroy();
		}

		const stdioShutdown = () => quitAndDestroy();
		if (usesProcessStdio) {
			process.stdin.once("end", stdioShutdown);
			process.stdin.once("close", stdioShutdown);
			process.stdin.once("error", stdioShutdown);
			process.stdout.once("error", stdioShutdown);
			process.stderr.once("error", stdioShutdown);
		}

		const alive = new Promise<void>((resolve) => {
			resolveAlive = resolve;
			// Wire the completion resolver before exposing the renderer. A remote
			// shutdown can destroy it synchronously from this callback.
			opts?.terminal?.onRendererReady?.(renderer);
			if (renderer.isDestroyed) return;

			render(
				() => (
					<KeymapProvider keymap={keymap}>
						<App
							settings={settings}
							session={session}
							persistSession={opts?.noSession !== true}
							updateTerminalTitle={updateTerminalTitle}
							setTerminalTurnActive={(active) => {
								setTerminalTitleTurnActive(active);
								setTerminalProgress(active ? "indeterminate" : "remove");
							}}
							triggerNotification={(message, title) =>
								opts?.terminal?.notify?.(message, title) ??
								renderer.triggerNotification(message, title)
							}
							triggerBell={opts?.terminal?.bell ?? ringLocalBell}
							copyText={opts?.terminal?.copyText ?? copyToClipboard}
							quitAndDestroy={quitAndDestroy}
							registerDispose={(dispose) => {
								disposeApp = dispose;
							}}
						/>
					</KeymapProvider>
				),
				renderer,
			);
		});

		await alive;
		if (usesProcessStdio) {
			process.stdin.off("end", stdioShutdown);
			process.stdin.off("close", stdioShutdown);
			process.stdin.off("error", stdioShutdown);
			process.stdout.off("error", stdioShutdown);
			process.stderr.off("error", stdioShutdown);
		}
	} catch (error) {
		if (!renderer.isDestroyed) renderer.destroy();
		throw error;
	}
}
