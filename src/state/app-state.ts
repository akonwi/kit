import { homedir } from "node:os";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { createStore } from "solid-js/store";
import type { AgentRuntime, RuntimeStatus } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";

export type DockMode = "composer" | "wizard" | "pager";

export type PanelState = {
  visible: boolean;
  title: string;
  lines: string[];
};

export type ComposerState = {
  mode: DockMode;
  title: string;
  placeholder: string;
  text: string;
  height: number;
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
  composer: ComposerState;
  footerStatus: FooterStatusState;
  sessionMeta: SessionMeta;
  /** Debug: raw message JSON for the currently inspected message */
  debugEntry: string | null;
};

function formatCwd(rawCwd: string): string {
  const home = homedir();
  return rawCwd.startsWith(home) ? `~${rawCwd.slice(home.length)}` : rawCwd;
}

function deriveFooterStatus(runtime: AgentRuntime | null): Omit<FooterStatusState, "cwd"> {
  if (runtime) {
    const status = runtime.getStatus();
    return {
      model: status.model,
      thinkingLevel: status.thinkingLevel,
      contextPct: status.contextPct,
    };
  }
  return {
    model: "no-model",
    thinkingLevel: "off",
    contextPct: "–",
  };
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

export function buildInitialAppState(
  _settings: LoadedSettings,
  session: LoadedSession | null,
  runtime: AgentRuntime | null,
): AppState {
  const messages = runtime ? runtime.getMessages() : [];
  const footer = deriveFooterStatus(runtime);

  return {
    messages,
    panel: {
      visible: false,
      title: "",
      lines: [],
    },
    composer: {
      mode: "composer",
      title: "Compose",
      placeholder: "Ask pi-kit to do something...",
      text: "",
      height: 6,
    },
    footerStatus: {
      cwd: formatCwd(process.cwd()),
      ...footer,
    },
    sessionMeta: buildSessionMeta(session),
    debugEntry: null,
  };
}

export function createAppState(
  settings: LoadedSettings,
  session: LoadedSession | null,
  runtime: AgentRuntime | null,
) {
  const [state, setState] = createStore(buildInitialAppState(settings, session, runtime));

  function showPanel(title: string, lines: string[]) {
    setState("panel", {
      visible: true,
      title,
      lines,
    });
  }

  function hidePanel() {
    setState("panel", {
      visible: false,
      title: "",
      lines: [],
    });
  }

  runtime?.subscribe((event) => {
    switch (event.type) {
      case "messages_changed":
        setState("messages", event.messages);
        break;
      case "status_changed":
        setState("footerStatus", applyRuntimeStatus(state.footerStatus, event.status));
        break;
      case "panel":
        setState("panel", event.panel);
        break;
      case "error":
        showPanel(event.title, event.lines);
        break;
      default:
        break;
    }
  });

  function inspectMessage(msg: AgentMessage) {
    const json = JSON.stringify(msg, null, 2);
    const current = state.debugEntry;
    setState("debugEntry", current === json ? null : json);
  }

  function setComposerText(text: string) {
    setState("composer", "text", text);
  }

  function handleSlashCommand(raw: string) {
    const [command] = raw.trim().split(/\s+/, 1);
    showPanel("Commands", [`${command} is not implemented yet.`]);
    setState("composer", "text", "");
  }

  async function submitComposer() {
    const raw = state.composer.text;
    if (!raw.trim()) return;

    if (raw.trimStart().startsWith("/")) {
      handleSlashCommand(raw);
      return;
    }

    if (!runtime) {
      showPanel("Runtime Error", ["No runtime is available for this submission."]);
      return;
    }

    setState("composer", "text", "");

    try {
      await runtime.submitUserMessage(raw);
    } catch (error) {
      setState("composer", "text", raw);
      if (error instanceof Error) {
        showPanel("Runtime Error", [error.message]);
      } else {
        showPanel("Runtime Error", [String(error)]);
      }
    }
  }

  return {
    state,
    setState,
    inspectMessage,
    setComposerText,
    submitComposer,
    showPanel,
    hidePanel,
  };
}
