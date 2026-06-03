import path from "node:path";
import { parseArgs } from "node:util";
import {
	ConsolePosition,
	createCliRenderer,
	getTreeSitterClient,
} from "@opentui/core";
import { KeymapProvider } from "@opentui/keymap/solid";
import { render } from "@opentui/solid";
import { createKitKeymap } from "../keymap/setup";
import { getInstalledRuntimeDir } from "../runtime/runtime-dir";
import type { Session } from "../session";
import { loadSettings } from "../settings";
import { initTemplates } from "../shell/templates";
import {
	initTerminalTitle,
	updateTerminalTitle,
} from "../shell/terminal-title";
import { resolveAndApplyTheme } from "../shell/theme";
import { App } from "./App";

type ProcessWithActiveHandles = NodeJS.Process & {
	_getActiveHandles?: () => unknown[];
	_getActiveRequests?: () => unknown[];
};

function describeActiveHandle(handle: unknown): string {
	if (typeof handle !== "object" || handle === null) return typeof handle;
	const constructorName = handle.constructor?.name;
	return constructorName || Object.prototype.toString.call(handle);
}

function reportDanglingHandlesForDebugging(): void {
	if (!process.env.KIT_DEBUG_SHUTDOWN) return;
	const proc = process as ProcessWithActiveHandles;
	const handles = proc._getActiveHandles?.() ?? [];
	const requests = proc._getActiveRequests?.() ?? [];
	console.error(
		`[kit] forcing shutdown with ${handles.length} active handle(s), ${requests.length} active request(s)`,
	);
	for (const handle of handles) {
		console.error(`[kit] active handle: ${describeActiveHandle(handle)}`);
	}
	for (const request of requests) {
		console.error(`[kit] active request: ${describeActiveHandle(request)}`);
	}
}

type BootstrapOpts = {
	sessionId?: string;
	newSession?: boolean;
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

	if (opts?.newSession) {
		const { createSession } = await import("../session");
		return createSession(process.cwd());
	}

	const { openRecentSession } = await import("../session");
	return openRecentSession(process.cwd());
}

export async function bootstrap(opts?: BootstrapOpts): Promise<void> {
	// Legacy support for older wrapper-based launches. The compiled binary should
	// run directly from the user's current working directory and not need this.
	const userCwd = process.env.KIT_USER_CWD;
	if (userCwd && userCwd !== process.cwd()) {
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
		{ filetype: "bash", wasm: "tree-sitter-bash.wasm" },
		{ filetype: "css", wasm: "tree-sitter-css.wasm" },
		{ filetype: "go", wasm: "tree-sitter-go.wasm" },
		{ filetype: "html", wasm: "tree-sitter-html.wasm" },
		{ filetype: "json", wasm: "tree-sitter-json.wasm" },
		{ filetype: "python", wasm: "tree-sitter-python.wasm" },
		{ filetype: "ruby", wasm: "tree-sitter-ruby.wasm" },
		{ filetype: "rust", wasm: "tree-sitter-rust.wasm" },
		{ filetype: "toml", wasm: "tree-sitter-toml.wasm" },
		{ filetype: "yaml", wasm: "tree-sitter-yaml.wasm" },
	]) {
		treeSitter.addFiletypeParser({
			filetype: grammar.filetype,
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

	const renderer = await createCliRenderer({
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
		consoleOptions: {
			position: ConsolePosition.TOP,
			sizePercent: 30,
		},
	});
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

	initTerminalTitle((title) => renderer.setTerminalTitle(title));
	updateTerminalTitle(session.name, session.cwd);
	initTemplates(session.cwd);

	// Keep the process alive until the renderer is destroyed.
	// In compiled binaries, the async bootstrap() returning would let
	// the event loop drain and the process exit prematurely.
	let quitStarted = false;
	const alive = new Promise<void>((resolve) => {
		function quitAndDestroy(): void {
			if (quitStarted) return;
			quitStarted = true;
			renderer.destroy();
			resolve();

			// OpenTUI generally discourages process.exit() because it can bypass
			// terminal cleanup and leave the user's shell in raw/alternate-screen state.
			// This watchdog only runs after renderer.destroy() has restored the terminal
			// and the normal bootstrap promise has resolved. It exists because Bun or a
			// native integration can still keep the process alive with no visible active
			// handles, leaving users at a hung shell after quitting.
			const shutdownWatchdog = setTimeout(() => {
				reportDanglingHandlesForDebugging();
				process.exit(0);
			}, 200);
			shutdownWatchdog.unref?.();
		}

		render(
			() => (
				<KeymapProvider keymap={keymap}>
					<App
						settings={settings}
						session={session}
						updateTerminalTitle={updateTerminalTitle}
						triggerNotification={(message, title) =>
							renderer.triggerNotification(message, title)
						}
						quitAndDestroy={quitAndDestroy}
					/>
				</KeymapProvider>
			),
			renderer,
		);
	});

	await alive;
}
