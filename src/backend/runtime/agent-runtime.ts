import type { AgentMessage, ThinkingLevel } from "@mariozechner/pi-agent-core";
import type { Api, Model } from "@mariozechner/pi-ai";
import {
  AgentSession,
  createAgentSession,
  SessionManager,
  type AgentSessionEvent,
} from "@mariozechner/pi-coding-agent";
import {
  snapshotLoadedSession,
  type LoadedSession,
} from "../../compat/sessions";

export type RuntimePanelState = {
  visible: boolean;
  title: string;
  lines: string[];
};

export type RuntimeStatus = {
  model: string;
  thinkingLevel: string;
  contextPct: string;
  isStreaming: boolean;
};

export type AgentRuntimeEvent =
  | { type: "messages_changed"; messages: AgentMessage[] }
  | { type: "status_changed"; status: RuntimeStatus }
  | { type: "panel"; panel: RuntimePanelState }
  | { type: "error"; title: string; lines: string[] };

export type AgentRuntime = {
  submitUserMessage(text: string): Promise<void>;
  newSession(): Promise<boolean>;
  getAvailableModels(): Array<{ id: string; name: string; provider: string }>;
  getCurrentModelId(): string | undefined;
  setModel(provider: string, modelId: string): Promise<void>;
  cycleModel(direction?: "forward" | "backward"): Promise<string | undefined>;
  setThinkingLevel(level: ThinkingLevel): void;
  cycleThinkingLevel(): string | undefined;
  setSessionName(name: string): void;
  onQuit(handler: () => void): void;
  quit(): void;
  subscribe(listener: (event: AgentRuntimeEvent) => void): () => void;
  getSession(): LoadedSession;
  getAgentSession(): AgentSession;
  getMessages(): AgentMessage[];
  getStatus(): RuntimeStatus;
  dispose(): void;
};

function panel(title: string, lines: string[]): RuntimePanelState {
  return { visible: true, title, lines };
}

function hiddenPanel(): RuntimePanelState {
  return { visible: false, title: "", lines: [] };
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

export async function createAgentRuntime(initialSession: LoadedSession | null): Promise<AgentRuntime> {
  const sessionManager = initialSession?.manager ?? SessionManager.continueRecent(process.cwd());
  const { session: agentSession } = await createAgentSession({
    cwd: process.cwd(),
    sessionManager,
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
    return {
      model: model?.name ?? model?.id ?? "no-model",
      thinkingLevel: agentSession.thinkingLevel ?? "off",
      contextPct: usage?.percent != null ? `${Math.round(usage.percent)}%` : "–",
      isStreaming: agentSession.isStreaming,
    };
  }

  function emitMessages() {
    emit({ type: "messages_changed", messages: [...agentSession.messages] });
  }

  function emitStatus() {
    emit({ type: "status_changed", status: snapshotStatus() });
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
        // User message is already in state.messages at this point
        emitMessages();
        emit({ type: "panel", panel: panel("", ["Working..."]) });
        break;
      case "message_start":
        if (event.message.role === "assistant") {
          emit({ type: "panel", panel: panel("", ["Thinking..."]) });
        }
        break;
      case "message_end":
        emitMessages();
        emitStatus();
        break;
      case "tool_execution_start":
        emit({
          type: "panel",
          panel: panel("", [summarizeToolStart(event.toolName, event.args)]),
        });
        break;
      case "tool_execution_end":
        emit({
          type: "panel",
          panel: panel("", [summarizeToolEnd(event.toolName, event.isError)]),
        });
        break;
      case "agent_end":
        currentSession = snapshotLoadedSession(agentSession.sessionManager);
        emitMessages();
        emitStatus();
        emit({ type: "panel", panel: hiddenPanel() });
        break;
      default:
        break;
    }
  }

  return {
    async submitUserMessage(text: string): Promise<void> {
      try {
        await agentSession.sendUserMessage(text);
      } catch (error) {
        const runtimeError = defaultRuntimeError(error);
        emit({ type: "error", title: runtimeError.title, lines: runtimeError.lines });
        throw error;
      }
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
    async newSession(): Promise<boolean> {
      const ok = await agentSession.newSession();
      if (ok) {
        currentSession = snapshotLoadedSession(agentSession.sessionManager);
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
