import { rm } from "node:fs/promises";
import type { AgentMessage, ThinkingLevel } from "@mariozechner/pi-agent-core";
import type { Api, Model } from "@mariozechner/pi-ai";
import {
  AgentSession,
  createAgentSession,
  SessionManager,
  type AgentSessionEvent,
  type SessionInfo,
} from "@mariozechner/pi-coding-agent";

// Minimal BashResult type - just what we need
export interface BashResult {
  output: string;
  exitCode: number | undefined;
  cancelled: boolean;
  truncated: boolean;
  fullOutputPath?: string;
}
import {
  snapshotLoadedSession,
  type LoadedSession,
} from "../../compat/sessions";
import { expandThreadReferences } from "../../features/threads";
import { getGitInfo, type GitInfo } from "./git-info";

export type RuntimePanelState = {
  pending: boolean;
  title: string;
};

export type RuntimeStatus = {
  model: string;
  thinkingLevel: string;
  contextPct: string;
  isStreaming: boolean;
  git: GitInfo;
};

export type AgentRuntimeEvent =
  | { type: "messages_changed"; messages: AgentMessage[] }
  | { type: "status_changed"; status: RuntimeStatus }
  | { type: "session_changed"; session: LoadedSession }
  | { type: "panel"; panel: RuntimePanelState }
  | { type: "tool_completed" }
  | { type: "turn_complete"; messages: AgentMessage[] }
  | { type: "error"; title: string; lines: string[] };

export type AgentRuntime = {
  submitUserMessage(text: string): Promise<void>;
  executeBash(command: string, excludeFromContext?: boolean): Promise<BashResult>;
  abort(): Promise<void>;
  newSession(options?: { parentSession?: string; setup?: (sm: SessionManager) => Promise<void> }): Promise<boolean>;
  getAvailableModels(): Array<{ id: string; name: string; provider: string }>;
  getCurrentModelId(): string | undefined;
  setModel(provider: string, modelId: string): Promise<void>;
  cycleModel(direction?: "forward" | "backward"): Promise<string | undefined>;
  setThinkingLevel(level: ThinkingLevel): void;
  cycleThinkingLevel(): string | undefined;
  setSessionName(name: string): void;
  listAllSessions(): Promise<SessionInfo[]>;
  switchSession(sessionPath: string): Promise<boolean>;
  renameSession(sessionPath: string, name: string): void;
  deleteSession(sessionPath: string): Promise<void>;
  showPanel(title: string): void;
  hidePanel(): void;
  onQuit(handler: () => void): void;
  quit(): void;
  subscribe(listener: (event: AgentRuntimeEvent) => void): () => void;
  getSession(): LoadedSession;
  getAgentSession(): AgentSession;
  getMessages(): AgentMessage[];
  getStatus(): RuntimeStatus;
  dispose(): void;
};

function panelActive(title: string): RuntimePanelState {
  return { pending: true, title };
}

function panelIdle(): RuntimePanelState {
  return { pending: false, title: "" };
}

function summarizeToolStart(toolName: string, args: unknown): string {
  if (args && typeof args === "object") {
    const record = args as Record<string, unknown>;
    if (typeof record.command === "string" && record.command.trim()) {
      return `Running ${toolName}: ${record.command}`;
    }
    if (typeof record.path === "string" && record.path.trim()) {
      return `Running ${toolName}: ${record.path}`;
    }
  }
  return `Running ${toolName}...`;
}

function summarizeToolEnd(toolName: string, isError: boolean): string {
  return isError ? `✗ ${toolName} failed` : `✓ ${toolName} finished`;
}

function defaultRuntimeError(error: unknown): { title: string; lines: string[] } {
  if (error instanceof Error) {
    return { title: "Runtime Error", lines: [error.message] };
  }
  return { title: "Runtime Error", lines: [String(error)] };
}

export type AgentRuntimeOptions = {
  customTools?: any[];
};

export async function createAgentRuntime(
  initialSession: LoadedSession | null,
  options?: AgentRuntimeOptions,
): Promise<AgentRuntime> {
  const sessionManager = initialSession?.manager ?? SessionManager.continueRecent(process.cwd());
  const { session: agentSession } = await createAgentSession({
    cwd: process.cwd(),
    sessionManager,
    customTools: options?.customTools,
  });

  let currentSession = snapshotLoadedSession(agentSession.sessionManager);
  let quitHandler: (() => void) | null = null;
  const listeners = new Set<(event: AgentRuntimeEvent) => void>();

  const emit = (event: AgentRuntimeEvent) => {
    for (const listener of listeners) {
      listener(event);
    }
  };

  function snapshotStatus(): RuntimeStatus {
    const model = agentSession.model;
    const usage = agentSession.getContextUsage();
    const cwd = agentSession.sessionManager.getCwd();
    const git = getGitInfo(cwd);
    return {
      model: model?.name ?? model?.id ?? "no-model",
      thinkingLevel: agentSession.thinkingLevel ?? "off",
      contextPct: usage?.percent != null ? `${Math.round(usage.percent)}%` : "–",
      isStreaming: agentSession.isStreaming,
      git,
    };
  }

  function emitMessages() {
    emit({ type: "messages_changed", messages: [...agentSession.messages] });
  }

  function emitStatus() {
    emit({ type: "status_changed", status: snapshotStatus() });
  }

  function emitSession() {
    emit({ type: "session_changed", session: currentSession });
  }

  const unsubscribeAgent = agentSession.subscribe((event: AgentSessionEvent) => {
    try {
      handleAgentEvent(event);
    } catch (error) {
      const runtimeError = defaultRuntimeError(error);
      emit({ type: "error", title: runtimeError.title, lines: runtimeError.lines });
    }
  });

  function handleAgentEvent(event: AgentSessionEvent) {
    switch (event.type) {
      case "agent_start":
        emitMessages();
        emit({ type: "panel", panel: panelActive("Working...") });
        break;
      case "message_start":
        if (event.message.role === "assistant") {
          emit({ type: "panel", panel: panelActive("Thinking...") });
        }
        break;
      case "message_update": {
        const ame = (event as { assistantMessageEvent?: { type: string; delta?: string } }).assistantMessageEvent;
        if (ame?.type === "thinking_delta" && ame.delta) {
          const trimmed = ame.delta.replace(/\s+/g, " ").trim();
          if (trimmed) {
            emit({ type: "panel", panel: panelActive(trimmed) });
          }
        }
        break;
      }
      case "message_end":
        emitMessages();
        emitStatus();
        break;
      case "tool_execution_start":
        emit({ type: "panel", panel: panelActive(summarizeToolStart(event.toolName, event.args)) });
        break;
      case "tool_execution_end":
        emit({ type: "panel", panel: panelActive(summarizeToolEnd(event.toolName, event.isError)) });
        emit({ type: "tool_completed" });
        break;
      case "agent_end":
        currentSession = snapshotLoadedSession(agentSession.sessionManager);
        emitSession();
        emitMessages();
        emitStatus();
        emit({ type: "panel", panel: panelIdle() });
        emit({ type: "turn_complete", messages: [...agentSession.messages] });
        break;
      default:
        break;
    }
  }

  return {
    async submitUserMessage(text: string): Promise<void> {
      try {
        // Expand [[thread:id]] references before sending
        let finalText = text;
        if (text.includes("[[thread:")) {
          const currentPath = agentSession.sessionManager.getSessionFile();
          const result = await expandThreadReferences(text, currentPath);
          finalText = result.text;
          if (result.errors.length > 0) {
            emit({ type: "error", title: "Thread Reference", lines: result.errors });
          }
        }
        await agentSession.sendUserMessage(finalText);
      } catch (error) {
        const runtimeError = defaultRuntimeError(error);
        emit({ type: "error", title: runtimeError.title, lines: runtimeError.lines });
        throw error;
      }
    },
    async executeBash(command: string, excludeFromContext?: boolean): Promise<BashResult> {
      const result = await agentSession.executeBash(command, undefined, { excludeFromContext });
      return result;
    },
    async abort(): Promise<void> {
      await agentSession.abort();
    },
    getAvailableModels(): Array<{ id: string; name: string; provider: string }> {
      return agentSession.modelRegistry.getAvailable().map((m: Model<Api>) => ({
        id: m.id,
        name: m.name,
        provider: m.provider,
      }));
    },
    getCurrentModelId(): string | undefined {
      return agentSession.model?.id;
    },
    async setModel(provider: string, modelId: string): Promise<void> {
      const model = agentSession.modelRegistry.getAvailable().find(
        (m: Model<Api>) => m.provider === provider && m.id === modelId,
      );
      if (!model) {
        throw new Error(`Model not found: ${provider}/${modelId}`);
      }
      await agentSession.setModel(model);
      emitStatus();
    },
    async newSession(options?: { parentSession?: string; setup?: (sm: SessionManager) => Promise<void> }): Promise<boolean> {
      const ok = await agentSession.newSession({
        parentSession: options?.parentSession,
        setup: options?.setup,
      });
      if (ok) {
        currentSession = snapshotLoadedSession(agentSession.sessionManager);
        emitSession();
        emitMessages();
        emitStatus();
      }
      return ok;
    },
    async cycleModel(direction?: "forward" | "backward"): Promise<string | undefined> {
      const result = await agentSession.cycleModel(direction);
      emitStatus();
      return result ? result.model.name ?? result.model.id : undefined;
    },
    setThinkingLevel(level: ThinkingLevel): void {
      agentSession.setThinkingLevel(level);
      emitStatus();
    },
    cycleThinkingLevel(): string | undefined {
      const level = agentSession.cycleThinkingLevel();
      emitStatus();
      return level;
    },
    setSessionName(name: string): void {
      agentSession.setSessionName(name);
      currentSession = snapshotLoadedSession(agentSession.sessionManager);
      emitSession();
    },
    renameSession(sessionPath: string, name: string): void {
      const isCurrent = agentSession.sessionFile === sessionPath;
      if (isCurrent) {
        agentSession.setSessionName(name);
        currentSession = snapshotLoadedSession(agentSession.sessionManager);
        emitSession();
      } else {
        const sm = SessionManager.open(sessionPath);
        sm.appendSessionInfo(name);
      }
    },
    async deleteSession(sessionPath: string): Promise<void> {
      if (agentSession.sessionFile === sessionPath) {
        throw new Error("Cannot delete the currently active session.");
      }
      await rm(sessionPath);
    },
    async listAllSessions(): Promise<SessionInfo[]> {
      return SessionManager.listAll();
    },
    async switchSession(sessionPath: string): Promise<boolean> {
      const ok = await agentSession.switchSession(sessionPath);
      if (ok) {
        currentSession = snapshotLoadedSession(agentSession.sessionManager);
        emitSession();
        emitMessages();
        emitStatus();
      }
      return ok;
    },
    showPanel(title: string) {
      emit({ type: "panel", panel: panelActive(title) });
    },
    hidePanel() {
      emit({ type: "panel", panel: panelIdle() });
    },
    onQuit(handler: () => void) {
      quitHandler = handler;
    },
    quit() {
      quitHandler?.();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    getSession() {
      return currentSession;
    },
    getAgentSession() {
      return agentSession;
    },
    getMessages() {
      return [...agentSession.messages];
    },
    getStatus() {
      return snapshotStatus();
    },
    dispose() {
      unsubscribeAgent();
      agentSession.dispose();
      listeners.clear();
    },
  };
}
