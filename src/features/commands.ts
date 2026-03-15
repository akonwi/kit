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
  panel?: { title: string; lines: string[] };
  /** If set, update the session name label in the UI */
  sessionName?: string;
  /** If set, open a model picker with these items */
  openModelPicker?: {
    models: ModelPickerItem[];
    currentModelId: string | undefined;
  };
  /** If set, open a session picker with these items */
  openSessionPicker?: {
    sessions: SessionPickerItem[];
    currentSessionId: string | undefined;
  };
};

const THINKING_LEVELS: ThinkingLevel[] = ["off", "minimal", "low", "medium", "high", "xhigh"];

function isThinkingLevel(value: string): value is ThinkingLevel {
  return THINKING_LEVELS.includes(value as ThinkingLevel);
}

export async function executeCommand(
  raw: string,
  runtime: AgentRuntime,
): Promise<CommandResult> {
  const trimmed = raw.trim();
  const spaceIdx = trimmed.indexOf(" ");
  const command = spaceIdx === -1 ? trimmed : trimmed.slice(0, spaceIdx);
  const args = spaceIdx === -1 ? "" : trimmed.slice(spaceIdx + 1).trim();

  switch (command) {
    case "/new":
      return handleNew(runtime);
    case "/model":
      return handleModel(runtime, args);
    case "/thinking":
      return handleThinking(runtime, args);
    case "/name":
      return handleName(runtime, args);
    case "/session":
      return handleSession(runtime);
    case "/switch":
      return handleSwitch(runtime);
    case "/quit":
    case "/exit":
      runtime.quit();
      return {};
    default:
      return { panel: { title: "", lines: [`Unknown command: ${command}`] } };
  }
}

async function handleNew(runtime: AgentRuntime): Promise<CommandResult> {
  const ok = await runtime.newSession();
  if (ok) {
    return { panel: { title: "", lines: ["Started new session."] } };
  }
  return { panel: { title: "", lines: ["Could not start new session."] } };
}

async function handleModel(runtime: AgentRuntime, _args: string): Promise<CommandResult> {
  const models = runtime.getAvailableModels();
  if (models.length === 0) {
    return { panel: { title: "", lines: ["No models available."] } };
  }
  return {
    openModelPicker: {
      models,
      currentModelId: runtime.getCurrentModelId(),
    },
  };
}

async function handleThinking(runtime: AgentRuntime, args: string): Promise<CommandResult> {
  if (args && isThinkingLevel(args)) {
    runtime.setThinkingLevel(args);
    return { panel: { title: "", lines: [`Thinking → ${args}`] } };
  }
  if (args) {
    return {
      panel: {
        title: "",
        lines: [`Invalid thinking level: ${args}. Valid: ${THINKING_LEVELS.join(", ")}`],
      },
    };
  }
  // No args — cycle
  const level = runtime.cycleThinkingLevel();
  if (level) {
    return { panel: { title: "", lines: [`Thinking → ${level}`] } };
  }
  return { panel: { title: "", lines: ["Current model does not support thinking."] } };
}

function handleName(runtime: AgentRuntime, args: string): CommandResult {
  if (!args) {
    return { panel: { title: "", lines: ["Usage: /name <session name>"] } };
  }
  runtime.setSessionName(args);
  return {
    panel: { title: "", lines: [`Session name → ${args}`] },
    sessionName: args,
  };
}

async function handleSwitch(runtime: AgentRuntime): Promise<CommandResult> {
  const sessions = await runtime.listAllSessions();
  if (sessions.length === 0) {
    return { panel: { title: "", lines: ["No sessions found."] } };
  }

  // Sort by most recently modified first
  const sorted = [...sessions].sort((a, b) => b.modified.getTime() - a.modified.getTime());
  const currentId = runtime.getSession().sessionId;

  const items: SessionPickerItem[] = sorted.map((s) => ({
    path: s.path,
    id: s.id,
    name: s.name,
    cwd: s.cwd,
    modified: s.modified,
    firstMessage: s.firstMessage,
  }));

  return {
    openSessionPicker: {
      sessions: items,
      currentSessionId: currentId,
    },
  };
}

async function handleSession(runtime: AgentRuntime): Promise<CommandResult> {
  const session = runtime.getAgentSession();
  const stats = session.getSessionStats();
  const lines: string[] = [
    `Session: ${stats.sessionId}`,
    `File: ${stats.sessionFile ?? "(unsaved)"}`,
    `Messages: ${stats.totalMessages} (${stats.userMessages} user, ${stats.assistantMessages} assistant)`,
    `Tool calls: ${stats.toolCalls}`,
  ];
  if (stats.tokens.total > 0) {
    lines.push(`Tokens: ${stats.tokens.total.toLocaleString()} (in: ${stats.tokens.input.toLocaleString()}, out: ${stats.tokens.output.toLocaleString()})`);
  }
  if (stats.cost > 0) {
    lines.push(`Cost: $${stats.cost.toFixed(4)}`);
  }
  return { panel: { title: "", lines } };
}
