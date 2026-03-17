import path from "node:path";
import { parseArgs } from "node:util";
import { ConsolePosition, createCliRenderer, getTreeSitterClient } from "@opentui/core";
import { render } from "@opentui/solid";
import { createAgentRuntime } from "../backend";
import {
  openRecentSession,
  openSessionFile,
  findSessionFileById,
  type LoadedSession,
} from "../compat/sessions";
import { loadSettings } from "../compat/settings/load-settings";
import { App } from "./App";

function loadSession(): LoadedSession | null {
  const { values } = parseArgs({
    args: process.argv.slice(2),
    options: {
      session: { type: "string", short: "s" },
    },
    strict: false,
  });

  const sessionArg = values.session as string | undefined;

  if (sessionArg) {
    // If it looks like a file path, open it directly
    if (sessionArg.endsWith(".jsonl") || sessionArg.includes("/")) {
      return openSessionFile(sessionArg);
    }

    // Otherwise treat it as a session UUID (or prefix)
    const filePath = findSessionFileById(sessionArg);
    if (!filePath) {
      console.error(`Session not found: ${sessionArg}`);
      process.exit(1);
    }
    return openSessionFile(filePath);
  }

  // Default: open the most recent session for the current cwd
  try {
    return openRecentSession(process.cwd());
  } catch {
    return null;
  }
}

export async function bootstrap(): Promise<void> {
  // Initialize tree-sitter and register additional filetype aliases.
  // Built-in markdown injection only maps ts/js/typescript/javascript,
  // so tsx/jsx need explicit registration using the same grammars.
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
  const session = loadSession();
  const runtime = await createAgentRuntime(session);
  const renderer = await createCliRenderer({
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
  runtime.onQuit(() => {
    runtime.dispose();
    renderer.destroy();
  });

  render(
    () => <App settings={settings} session={session} runtime={runtime} />,
    renderer,
  );
}
