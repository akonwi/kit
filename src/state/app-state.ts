import { homedir } from "node:os";
import { createStore } from "solid-js/store";
import type { LoadedSession } from "../compat/sessions";
import { mapBranchToTranscript } from "../compat/sessions/transcript-mapper";
import type { LoadedSettings } from "../compat/settings/load-settings";

export type TranscriptRole = "system" | "user" | "assistant" | "tool" | "meta";

export type TranscriptItem = {
  id: string;
  role: TranscriptRole;
  lines: string[];
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
  initialValue: string;
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
  // Try to get model from the session context (most recent model_change)
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
  // Try to get thinking level from the session context
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

  // No session — show a welcome message
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
      initialValue: "",
      height: 6,
    },
    footerStatus: {
      cwd: formatCwd(session?.cwd ?? process.cwd()),
      model: deriveModel(settings, session),
      thinkingLevel: deriveThinkingLevel(settings, session),
      contextPct: "0%",
      bellStatus: deriveBellStatus(settings),
      speechStatus: deriveSpeechStatus(settings),
      repoSummary: "",
    },
    sessionMeta: buildSessionMeta(session),
  };
}

export function createAppState(settings: LoadedSettings, session: LoadedSession | null) {
  const [state, setState] = createStore(buildInitialAppState(settings, session));
  return {
    state,
    setState,
  };
}
