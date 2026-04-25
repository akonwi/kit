import { existsSync, realpathSync } from "node:fs";
import path from "node:path";
import { parseArgs } from "node:util";
import {
	ConsolePosition,
	createCliRenderer,
	getTreeSitterClient,
} from "@opentui/core";
import { render } from "@opentui/solid";
import type { Session } from "../session";
import { loadSettings } from "../settings";
import { resolveAndApplyTheme } from "../shell/theme";
import {
	initTerminalTitle,
	updateTerminalTitle,
} from "../shell/terminal-title";
import { App } from "./App";

function getInstalledRuntimeDir(): string | null {
	try {
		const execDir = path.dirname(realpathSync(process.execPath));
		const runtimeDir = path.join(execDir, "runtime");
		return existsSync(runtimeDir) ? runtimeDir : null;
	} catch {
		return null;
	}
}

async function loadSession(sessionId?: string): Promise<Session> {
	// If a session ID is provided directly (e.g. from `kit threads`), use it
	const sessionArg =
		sessionId ??
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

	const { openRecentSession } = await import("../session");
	return openRecentSession(process.cwd());
}

export async function bootstrap(opts?: { sessionId?: string }): Promise<void> {
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

	const settings = await loadSettings();
	const session = await loadSession(opts?.sessionId);

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
	renderer.keyInput.on("keypress", (key) => {
		if (key.ctrl && key.name === "d") {
			renderer.console.toggle();
		}
	});

	// Resolve theme before rendering — "system" theme needs the renderer for palette detection
	const themeName = settings.settings.theme ?? "system";
	await resolveAndApplyTheme(themeName, renderer);

	initTerminalTitle((title) => renderer.setTerminalTitle(title));
	updateTerminalTitle(session.name, process.cwd());

	// Keep the process alive until the renderer is destroyed.
	// In compiled binaries, the async bootstrap() returning would let
	// the event loop drain and the process exit prematurely.
	const alive = new Promise<void>((resolve) => {
		render(
			() => (
				<App
					settings={settings}
					session={session}
					updateTerminalTitle={updateTerminalTitle}
					quitAndDestroy={() => {
						renderer.destroy();
						resolve();
					}}
				/>
			),
			renderer,
		);
	});

	await alive;
}
