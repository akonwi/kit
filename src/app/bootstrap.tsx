import { parseArgs } from "node:util";
import { createCliRenderer } from "@opentui/core";
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
  const settings = await loadSettings();
  const session = loadSession();
  const runtime = await createAgentRuntime(session);
  const renderer = await createCliRenderer();

  runtime.onQuit(() => {
    runtime.dispose();
    renderer.destroy();
  });

  render(() => <App settings={settings} session={session} runtime={runtime} />, renderer);
}
