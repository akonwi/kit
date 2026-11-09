// The subagents a session started, as the strip under its title lists them,
// and what the agent bar says about the one in view. The snapshot's
// conversations are authoritative; this only orders and names them, as the
// iOS app's SubagentStrip and SubagentFocus do.
import type { ModelCapability, SubagentConversation } from "../api/types";
import { THINKING_LABELS } from "../session/models";
import type { Row } from "../session/projection";

/** How an agent's button shows its work. */
export type AgentStatus = "working" | "queued" | "failed" | "interrupted" | "stopped" | "done";

export function agentStatus(agent: SubagentConversation): AgentStatus {
  switch (agent.state) {
    case "running":
      return "working";
    case "failed":
      return "failed";
    case "interrupted":
      return "interrupted";
    case "aborted":
      return "stopped";
    default:
      return hasOutstandingWork(agent) ? "queued" : "done";
  }
}

/** Whether the agent has work running or queued. */
export function hasOutstandingWork(agent: SubagentConversation): boolean {
  return agent.state === "running" || Boolean(agent.activeTaskId) || agent.queuedTasks > 0;
}

/** Agents that need a look, then working ones, then idle ones. */
function group(agent: SubagentConversation): number {
  if (agent.state === "failed" || agent.state === "interrupted") return 0;
  return hasOutstandingWork(agent) ? 1 : 2;
}

const START_ACTIONS = new Set(["start", "run", "spawn"]);

/** The agent a subagent call names, when it starts one. */
export function startedAgent(name: string, args: string | undefined): string | undefined {
  if (name !== "subagent" || !args) return undefined;
  try {
    const value = JSON.parse(args) as { action?: unknown; agent?: unknown };
    return START_ACTIONS.has(String(value.action)) && typeof value.agent === "string" && value.agent
      ? value.agent
      : undefined;
  } catch {
    return undefined;
  }
}

/**
 * Each agent's place among the transcript's Start agent calls. Other calls
 * naming an agent, such as a message to it, don't count.
 */
export function startOrder(rows: readonly Row[]): Map<string, number> {
  const order = new Map<string, number>();
  for (const row of rows) {
    if (row.kind !== "tools") continue;
    for (const tool of row.tools) {
      if (tool.status === "notRun") continue;
      const agent = startedAgent(tool.name, tool.arguments);
      if (agent && !order.has(agent)) order.set(agent, order.size);
    }
  }
  return order;
}

/**
 * The started agents in the strip's order. Within a group they keep the
 * order the main agent started them, so a button moves only when its group
 * changes; agents the loaded transcript doesn't start, such as ones started
 * before its first page, follow by name.
 */
export function stripAgents(
  conversations: readonly SubagentConversation[],
  rows: readonly Row[],
): SubagentConversation[] {
  const order = startOrder(rows);
  const seen = new Set<string>();
  const started = conversations.filter((agent) => {
    if (!agent.id || !agent.agentName || seen.has(agent.agentName)) return false;
    seen.add(agent.agentName);
    return true;
  });
  return started.sort((a, b) => {
    const byGroup = group(a) - group(b);
    if (byGroup !== 0) return byGroup;
    const x = order.get(a.agentName);
    const y = order.get(b.agentName);
    if (x !== undefined && y !== undefined) return x - y;
    if (x !== undefined) return -1;
    if (y !== undefined) return 1;
    return a.agentName.localeCompare(b.agentName, undefined, { numeric: true });
  });
}

/** A model's name from the catalog, or its id while the catalog loads. */
export function modelName(id: string, models: readonly ModelCapability[] | undefined): string {
  return models?.find((model) => model.id === id)?.name ?? id;
}

/** How the thinking level reads beside the model: `High`, `Thinking off`. */
export function thinkingName(level: string): string {
  return level === "off" ? "Thinking off" : (THINKING_LABELS[level] ?? level);
}

/** `reviewer · Claude Sonnet 4.5 · High`. */
export function agentLabel(
  agent: SubagentConversation,
  models: readonly ModelCapability[] | undefined,
): string {
  const parts = [agent.agentName];
  if (agent.model) parts.push(modelName(agent.model, models));
  if (agent.thinkingLevel) parts.push(thinkingName(agent.thinkingLevel));
  return parts.join(" · ");
}

/** How the agent's last task ended, when that needs saying. */
export function ending(agent: SubagentConversation): { text: string; tone: "danger" | "info" } | undefined {
  switch (agent.state) {
    case "failed":
      return { text: `${agent.agentName}’s last task failed.`, tone: "danger" };
    case "interrupted":
      return { text: `${agent.agentName}’s last task was interrupted.`, tone: "info" };
    case "aborted":
      return { text: `${agent.agentName}’s last task was stopped.`, tone: "info" };
    default:
      return undefined;
  }
}

/** Why the agent's settings can't change now, whichever one. */
function settingsLocked(agent: SubagentConversation, live: boolean, pending: boolean): string | undefined {
  if (!live) return "Reconnect to change";
  if (pending) return "Changing…";
  if (agent.generation <= 0) return "Unavailable";
  return undefined;
}

/**
 * Why the agent's model can't change now. The server takes a new model only
 * when the agent has no work running or queued.
 */
export function modelLocked(
  agent: SubagentConversation,
  live: boolean,
  pending: boolean,
): string | undefined {
  return (
    settingsLocked(agent, live, pending) ??
    (hasOutstandingWork(agent) ? `Can change once ${agent.agentName}’s work finishes` : undefined)
  );
}

/** Why the agent's thinking can't change now; its next request uses a new level. */
export function thinkingLocked(
  agent: SubagentConversation,
  live: boolean,
  pending: boolean,
): string | undefined {
  return settingsLocked(agent, live, pending);
}

/** Why the agent can't be dismissed now. */
export function dismissLocked(
  agent: SubagentConversation,
  live: boolean,
  pending: boolean,
): string | undefined {
  if (!live) return "Reconnect to dismiss";
  if (pending) return "Dismissing…";
  if (agent.generation <= 0) return "Unavailable";
  return undefined;
}
