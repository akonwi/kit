import { homedir } from "node:os";
import type { UserMessage } from "@mariozechner/pi-ai";
import { createStore } from "solid-js/store";
import type { LoadedSession } from "../compat/sessions";
import { mapBranchToTranscript } from "../compat/sessions/transcript-mapper";
import type { LoadedSettings } from "../compat/settings/load-settings";

export type TranscriptRole = "system" | "user" | "assistant" | "tool" | "meta";

export type TranscriptItem = {
  id: string;
  role: TranscriptRole;
  lines: string[];
  /** Raw session entry for debug inspection. Only present for session-loaded items. */
  rawEntry?: unknown;
};

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
  transcript: TranscriptItem[];
  panel: PanelState;
  composer: ComposerState;
  footerStatus: FooterStatusState;
  sessionMeta: SessionMeta;
  /** Debug: raw session entry JSON for the currently inspected transcript item */
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

function buildTranscript(session: LoadedSession | null): TranscriptItem[] {
  if (session && session.branch.length > 0) {
    return mapBranchToTranscript(session.branch);
  }

  return [
    {
      id: "welcome",
      role: "system",
      lines: ["pi-kit", "No existing session found. Start a conversation below."],
    },
  ];
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
): AppState {
  return {
    transcript: buildTranscript(session),
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

export function createAppState(settings: LoadedSettings, session: LoadedSession | null) {
  const [state, setState] = createStore(buildInitialAppState(settings, session));
  let activeSession = session;

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

  function refreshFromActiveSession() {
    setState("transcript", buildTranscript(activeSession));
    setState("sessionMeta", buildSessionMeta(activeSession));
    setState("footerStatus", {
      ...state.footerStatus,
      model: deriveModel(settings, activeSession),
      thinkingLevel: deriveThinkingLevel(settings, activeSession),
    });
  }

  function inspectTranscriptItem(item: TranscriptItem) {
    if (!item.rawEntry) return;
    const json = JSON.stringify(item.rawEntry, null, 2);
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

  function submitComposer() {
    const raw = state.composer.text;
    if (!raw.trim()) return;

    if (raw.trimStart().startsWith("/")) {
      handleSlashCommand(raw);
      return;
    }

    if (!activeSession) {
      showPanel("Session Error", ["No active session is available for this submission."]);
      return;
    }

    const userMessage: UserMessage = {
      role: "user",
      content: [{ type: "text", text: raw }],
      timestamp: Date.now(),
    };

    activeSession.manager.appendMessage(userMessage);
    activeSession = {
      ...activeSession,
      branch: activeSession.manager.getBranch(),
      sessionFile: activeSession.manager.getSessionFile(),
      sessionName: activeSession.manager.getSessionName(),
      sessionId: activeSession.manager.getSessionId(),
      cwd: activeSession.manager.getCwd(),
    };

    setState("composer", "text", "");
    hidePanel();
    refreshFromActiveSession();
  }

  return {
    state,
    setState,
    inspectTranscriptItem,
    setComposerText,
    submitComposer,
    showPanel,
    hidePanel,
  };
}
