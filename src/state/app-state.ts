import { homedir } from "node:os";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { createStore } from "solid-js/store";
import type { AgentRuntime } from "../backend";
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
  bellStatus: string;
  speechStatus: string;
  repoSummary: string;
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function asEnabledStatus(value: unknown, onLabel: string, offLabel: string): string {
  return value === true ? onLabel : offLabel;
}

function formatCwd(rawCwd: string): string {
  const home = homedir();
  return rawCwd.startsWith(home) ? `~${rawCwd.slice(home.length)}` : rawCwd;
}

function deriveModel(settings: LoadedSettings, session: LoadedSession | null): string {
  if (session) {
    const context = session.manager.buildSessionContext();
    if (context.model?.modelId) {
      return context.model.modelId;
    }
  }
  const raw = settings.values.model;
  return typeof raw === "string" && raw.trim() ? raw.trim() : "no-model";
}

function deriveThinkingLevel(settings: LoadedSettings, session: LoadedSession | null): string {
  if (session) {
    const context = session.manager.buildSessionContext();
    if (context.thinkingLevel) {
      return context.thinkingLevel;
    }
  }
  const raw = settings.values.thinking;
  return typeof raw === "string" && raw.trim() ? raw.trim() : "off";
}

function deriveBellStatus(settings: LoadedSettings): string {
  const bells = isRecord(settings.values.bells) ? settings.values.bells : null;
  return asEnabledStatus(bells?.enabled, "🔔 on", "🔕 off");
}

function deriveSpeechStatus(settings: LoadedSettings): string {
  const speech = isRecord(settings.values.speech) ? settings.values.speech : null;
  return asEnabledStatus(speech?.enabled, "🗣 on", "🤫 off");
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
  settings: LoadedSettings,
  session: LoadedSession | null,
  runtime: AgentRuntime | null,
): AppState {
  const messages = runtime ? runtime.getMessages() : [];

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
      model: deriveModel(settings, session),
      thinkingLevel: deriveThinkingLevel(settings, session),
      contextPct: "0%",
      bellStatus: deriveBellStatus(settings),
      speechStatus: deriveSpeechStatus(settings),
      repoSummary: "",
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
