import { existsSync, realpathSync } from "node:fs";
import path from "node:path";
import { parseArgs } from "node:util";
import {
	ConsolePosition,
	createCliRenderer,
	getTreeSitterClient,
} from "@opentui/core";
import { render } from "@opentui/solid";
import {
	createGuidedQuestionsController,
	createGuidedQuestionsTool,
	GUIDED_QUESTIONS_POLICY,
} from "../features/guided-questions";
import { createPagerController } from "../features/pager";
import { AgentRuntime } from "../runtime/agent-runtime";
import {
	findSessionById,
	openRecentSession,
	readSession,
	type Session,
} from "../session";
import { loadSettings } from "../settings";
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

async function loadSession(): Promise<Session> {
	const { values } = parseArgs({
		args: process.argv.slice(2),
		options: {
			session: { type: "string", short: "s" },
		},
		strict: false,
	});

	const sessionArg = values.session as string | undefined;

	if (sessionArg) {
		// Try as a UUID or prefix
		const session =
			(await findSessionById(sessionArg)) ?? (await readSession(sessionArg));
		if (!session) {
			console.error(`Session not found: ${sessionArg}`);
			process.exit(1);
		}
		return session;
	}

	// Default: open the most recent session for the current cwd
	return openRecentSession(process.cwd());
}

export async function bootstrap(): Promise<void> {
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
	const session = await loadSession();
	const guidedQuestions = createGuidedQuestionsController();
	const pager = createPagerController();
	const runtime = new AgentRuntime(session, {
		extraTools: [createGuidedQuestionsTool(guidedQuestions)],
		systemPromptAdditions: [GUIDED_QUESTIONS_POLICY],
	});
	pager.setSubmitCallback((msg) => runtime.submitUserMessage(msg));

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

	initTerminalTitle((title) => renderer.setTerminalTitle(title));
	updateTerminalTitle(session.name, process.cwd());

	runtime.onQuit(() => {
		runtime.dispose();
		renderer.destroy();
	});

	render(
		() => (
			<App
				settings={settings}
				session={session}
				runtime={runtime}
				guidedQuestions={guidedQuestions}
				pager={pager}
				updateTerminalTitle={updateTerminalTitle}
			/>
		),
		renderer,
	);
}
