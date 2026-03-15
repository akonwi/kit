import type { ThinkingLevel } from "@mariozechner/pi-agent-core";
import type { AgentRuntime } from "../backend";

export type ModelPickerItem = {
  id: string;
  name: string;
  provider: string;
};

export type SessionPickerItem = {
  path: string;
  id: string;
  name: string | undefined;
  cwd: string;
  modified: Date;
  firstMessage: string;
};

export type CommandResult = {
  openModelPicker?: {
    models: ModelPickerItem[];
    currentModelId: string | undefined;
  };
  openThinkingPicker?: {
    levels: ThinkingLevel[];
    current: string;
  };
  openNameInput?: {
    currentName: string;
  };
  openSessionPicker?: {
    sessions: SessionPickerItem[];
    currentSessionId: string | undefined;
  };
  openSessionManage?: {
    sessions: SessionPickerItem[];
    currentSessionId: string | undefined;
  };
};

const THINKING_LEVELS: ThinkingLevel[] = ["off", "minimal", "low", "medium", "high", "xhigh"];

export async function executeCommand(
  raw: string,
  runtime: AgentRuntime,
): Promise<CommandResult> {
  const command = raw.trim().split(/\s+/)[0];

  switch (command) {
    case "/new":
      await runtime.newSession();
      return {};
    case "/model":
      return handleModel(runtime);
    case "/thinking":
      return handleThinking(runtime);
    case "/name":
      return handleName(runtime);
    case "/switch":
      return handleSwitch(runtime);
    case "/sessions:manage":
      return handleSessionsManage(runtime);
    case "/quit":
    case "/exit":
      runtime.quit();
      return {};
    default:
      return {};
  }
}

function handleModel(runtime: AgentRuntime): CommandResult {
  const models = runtime.getAvailableModels();
  if (models.length === 0) return {};
  return {
    openModelPicker: {
      models,
      currentModelId: runtime.getCurrentModelId(),
    },
  };
}

function handleThinking(runtime: AgentRuntime): CommandResult {
  const current = runtime.getAgentSession().thinkingLevel ?? "off";
  return {
    openThinkingPicker: {
      levels: THINKING_LEVELS,
      current,
    },
  };
}

function handleName(runtime: AgentRuntime): CommandResult {
  const currentName = runtime.getSession().sessionName || "";
  return { openNameInput: { currentName } };
}

async function handleSwitch(runtime: AgentRuntime): Promise<CommandResult> {
  const sessions = await runtime.listAllSessions();
  if (sessions.length === 0) return {};

  const sorted = [...sessions].sort((a, b) => b.modified.getTime() - a.modified.getTime());
  const currentId = runtime.getSession().sessionId;

  return {
    openSessionPicker: {
      sessions: sorted.map((s) => ({
        path: s.path,
        id: s.id,
        name: s.name,
        cwd: s.cwd,
        modified: s.modified,
        firstMessage: s.firstMessage,
      })),
      currentSessionId: currentId,
    },
  };
}

async function handleSessionsManage(runtime: AgentRuntime): Promise<CommandResult> {
  const sessions = await runtime.listAllSessions();
  if (sessions.length === 0) return {};

  const sorted = [...sessions].sort((a, b) => b.modified.getTime() - a.modified.getTime());
  const currentId = runtime.getSession().sessionId;

  return {
    openSessionManage: {
      sessions: sorted.map((s) => ({
        path: s.path,
        id: s.id,
        name: s.name,
        cwd: s.cwd,
        modified: s.modified,
        firstMessage: s.firstMessage,
      })),
      currentSessionId: currentId,
    },
  };
}
