import type { ThinkingLevel } from "@mariozechner/pi-agent-core";
import type { AgentRuntime } from "../backend";

export type CommandResult = {
  panel?: { title: string; lines: string[] };
  /** If set, update the session name label in the UI */
  sessionName?: string;
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

async function handleModel(runtime: AgentRuntime, args: string): Promise<CommandResult> {
  if (args) {
    // For now, only cycling is supported. Setting by name would need model registry lookup.
    return { panel: { title: "", lines: ["Setting model by name is not yet supported. Use /model to cycle."] } };
  }
  const name = await runtime.cycleModel();
  if (name) {
    return { panel: { title: "", lines: [`Model → ${name}`] } };
  }
  return { panel: { title: "", lines: ["Only one model available."] } };
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
