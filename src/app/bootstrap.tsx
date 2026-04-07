import path from "node:path";
import { parseArgs } from "node:util";
import { ConsolePosition, createCliRenderer, getTreeSitterClient } from "@opentui/core";
import { render } from "@opentui/solid";
import { createAgentRuntime } from "../backend";
import { createWizardController, createGuidedQuestionsTool } from "../features/wizard";
import {
  openRecentSession,
  findSessionById,
  readSession,
  type Session,
} from "../session";
import { loadSettings } from "../compat/settings/load-settings";
import { loadNotificationConfig } from "../features/notification-config";
import { initTerminalTitle, updateTerminalTitle } from "../shell/terminal-title";
import { App } from "./App";

async function loadSession(): Promise<Session | null> {
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
    const session = await findSessionById(sessionArg) ?? await readSession(sessionArg);
    if (!session) {
      console.error(`Session not found: ${sessionArg}`);
      process.exit(1);
    }
    return session;
  }

  // Default: open the most recent session for the current cwd
  try {
    return await openRecentSession(process.cwd());
  } catch {
    return null;
  }
}

export async function bootstrap(): Promise<void> {
  // When launched via the bin script, CWD is the project root (for bunfig.toml).
  // Restore the user's actual working directory.
  const userCwd = process.env.KIT_USER_CWD;
  if (userCwd && userCwd !== process.cwd()) {
    process.chdir(userCwd);
  }

  // Initialize tree-sitter and register additional filetype aliases.
  const treeSitter = getTreeSitterClient();
  await treeSitter.initialize();

  const coreAssets = path.resolve(
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
  const notificationConfig = await loadNotificationConfig();
  const session = await loadSession();
  const wizard = createWizardController();
  const guidedQuestionsTool = createGuidedQuestionsTool(wizard);

  const runtime = await createAgentRuntime(session, {
    extraTools: [guidedQuestionsTool],
  });

  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    exitSignals: ["SIGTERM", "SIGQUIT", "SIGABRT", "SIGHUP", "SIGBREAK", "SIGPIPE", "SIGBUS", "SIGFPE"],
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
  updateTerminalTitle(session?.name, process.cwd());

  runtime.onQuit(() => {
    runtime.dispose();
    renderer.destroy();
  });

  render(
    () => <App settings={settings} session={session} runtime={runtime} wizard={wizard} notificationConfig={notificationConfig} updateTerminalTitle={updateTerminalTitle} />,
    renderer,
  );
}
