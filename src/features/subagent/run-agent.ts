/**
 * In-process subagent execution via createAgentSession.
 */

import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { Api, Model } from "@mariozechner/pi-ai";
import type { AgentTool } from "@mariozechner/pi-agent-core";
import {
  createAgentSession,
  SessionManager,
  SettingsManager,
  type AgentSession,
  type ModelRegistry,
  createReadTool,
  createBashTool,
  createEditTool,
  createWriteTool,
  createGrepTool,
  createFindTool,
  createLsTool,
  type ResourceLoader,
  createExtensionRuntime,
} from "@mariozechner/pi-coding-agent";
import type { AgentConfig } from "./agents";

// ── Types ────────────────────────────────────────────────────────

export interface UsageStats {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  contextTokens: number;
  turns: number;
}

export interface SingleResult {
  agent: string;
  agentSource: "user" | "project" | "unknown";
  task: string;
  exitCode: number;
  messages: AgentMessage[];
  usage: UsageStats;
  model?: string;
  stopReason?: string;
  errorMessage?: string;
  step?: number;
}

export type OnUpdateCallback = (partial: {
  content: Array<{ type: "text"; text: string }>;
  details: unknown;
}) => void;

// ── Helpers ──────────────────────────────────────────────────────

function emptyUsage(): UsageStats {
  return { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, contextTokens: 0, turns: 0 };
}

function getFinalOutput(messages: AgentMessage[]): string {
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i];
    if (msg.role === "assistant") {
      const content = (msg as any).content;
      if (Array.isArray(content)) {
        for (const part of content) {
          if (part.type === "text") return part.text;
        }
      }
    }
  }
  return "";
}

const TOOL_FACTORIES: Record<string, (cwd: string) => AgentTool<any>> = {
  read: createReadTool,
  bash: createBashTool,
  edit: createEditTool,
  write: createWriteTool,
  grep: createGrepTool,
  find: createFindTool,
  ls: createLsTool,
};

function resolveTools(cwd: string, toolNames?: string[]): AgentTool<any>[] {
  if (!toolNames || toolNames.length === 0) {
    // Default coding tools: read, bash, edit, write
    return [
      createReadTool(cwd),
      createBashTool(cwd),
      createEditTool(cwd),
      createWriteTool(cwd),
    ];
  }

  const tools: AgentTool<any>[] = [];
  for (const name of toolNames) {
    const factory = TOOL_FACTORIES[name];
    if (factory) {
      tools.push(factory(cwd));
    }
  }
  return tools;
}

function resolveModel(
  agentConfig: AgentConfig,
  modelRegistry: ModelRegistry,
  parentModel: Model<Api> | undefined,
): Model<Api> | undefined {
  if (agentConfig.model) {
    // Try to find the model by id across all providers
    const all = modelRegistry.getAvailable();
    const match = all.find((m) => m.id === agentConfig.model || m.name === agentConfig.model);
    if (match) return match;
  }
  return parentModel;
}

function makeMinimalResourceLoader(systemPrompt: string): ResourceLoader {
  return {
    getExtensions: () => ({
      extensions: [],
      errors: [],
      runtime: createExtensionRuntime(),
    }),
    getSkills: () => ({ skills: [], diagnostics: [] }),
    getPrompts: () => ({ prompts: [], diagnostics: [] }),
    getThemes: () => ({ themes: [], diagnostics: [] }),
    getAgentsFiles: () => ({ agentsFiles: [] }),
    getSystemPrompt: () => systemPrompt || "You are a helpful assistant. Be concise.",
    getAppendSystemPrompt: () => [],
    getPathMetadata: () => new Map(),
    extendResources: () => {},
    reload: async () => {},
  };
}

// ── Main runner ──────────────────────────────────────────────────

export async function runSingleAgent(opts: {
  cwd: string;
  agents: AgentConfig[];
  agentName: string;
  task: string;
  taskCwd?: string;
  step?: number;
  signal?: AbortSignal;
  onUpdate?: OnUpdateCallback;
  makeDetails: (results: SingleResult[]) => unknown;
  modelRegistry: ModelRegistry;
  parentModel: Model<Api> | undefined;
}): Promise<SingleResult> {
  const { cwd, agents, agentName, task, taskCwd, step, signal, onUpdate, makeDetails, modelRegistry, parentModel } = opts;

  const agent = agents.find((a) => a.name === agentName);
  if (!agent) {
    const available = agents.map((a) => `"${a.name}"`).join(", ") || "none";
    return {
      agent: agentName,
      agentSource: "unknown",
      task,
      exitCode: 1,
      messages: [],
      usage: emptyUsage(),
      errorMessage: `Unknown agent: "${agentName}". Available agents: ${available}.`,
      step,
    };
  }

  const effectiveCwd = taskCwd ?? cwd;
  const model = resolveModel(agent, modelRegistry, parentModel);
  const tools = resolveTools(effectiveCwd, agent.tools);
  const resourceLoader = makeMinimalResourceLoader(agent.systemPrompt);

  const currentResult: SingleResult = {
    agent: agentName,
    agentSource: agent.source,
    task,
    exitCode: 0,
    messages: [],
    usage: emptyUsage(),
    model: model?.name ?? model?.id,
    step,
  };

  const emitUpdate = () => {
    if (onUpdate) {
      onUpdate({
        content: [{ type: "text", text: getFinalOutput(currentResult.messages) || "(running...)" }],
        details: makeDetails([currentResult]),
      });
    }
  };

  let agentSession: AgentSession | undefined;

  try {
    const { session } = await createAgentSession({
      cwd: effectiveCwd,
      model,
      tools,
      resourceLoader,
      sessionManager: SessionManager.inMemory(),
      settingsManager: SettingsManager.inMemory({
        compaction: { enabled: false },
        retry: { enabled: false },
      }),
      modelRegistry,
    });
    agentSession = session;

    // Subscribe for progress updates
    session.subscribe((event) => {
      if (event.type === "message_end") {
        currentResult.messages = [...session.messages];
        const msg = (event as any).message;
        if (msg?.role === "assistant") {
          currentResult.usage.turns++;
          const usage = msg.usage;
          if (usage) {
            currentResult.usage.input += usage.input || 0;
            currentResult.usage.output += usage.output || 0;
            currentResult.usage.cacheRead += usage.cacheRead || 0;
            currentResult.usage.cacheWrite += usage.cacheWrite || 0;
            currentResult.usage.cost += usage.cost?.total || 0;
            currentResult.usage.contextTokens = usage.totalTokens || 0;
          }
          if (!currentResult.model && msg.model) currentResult.model = msg.model;
          if (msg.stopReason) currentResult.stopReason = msg.stopReason;
          if (msg.errorMessage) currentResult.errorMessage = msg.errorMessage;
        }
        emitUpdate();
      }
    });

    // Wire abort signal
    if (signal) {
      const abort = () => {
        session.abort().catch(() => {});
      };
      if (signal.aborted) {
        abort();
      } else {
        signal.addEventListener("abort", abort, { once: true });
      }
    }

    // Run the task
    await session.prompt(`Task: ${task}`);

    currentResult.messages = [...session.messages];
    emitUpdate();
    return currentResult;
  } catch (error) {
    currentResult.exitCode = 1;
    currentResult.errorMessage =
      error instanceof Error ? error.message : String(error);
    if (agentSession) {
      currentResult.messages = [...agentSession.messages];
    }
    return currentResult;
  } finally {
    agentSession?.dispose();
  }
}

// ── Concurrency helper ───────────────────────────────────────────

export async function mapWithConcurrencyLimit<TIn, TOut>(
  items: TIn[],
  concurrency: number,
  fn: (item: TIn, index: number) => Promise<TOut>,
): Promise<TOut[]> {
  if (items.length === 0) return [];
  const limit = Math.max(1, Math.min(concurrency, items.length));
  const results: TOut[] = new Array(items.length);
  let nextIndex = 0;
  const workers = new Array(limit).fill(null).map(async () => {
    while (true) {
      const current = nextIndex++;
      if (current >= items.length) return;
      results[current] = await fn(items[current], current);
    }
  });
  await Promise.all(workers);
  return results;
}

export { getFinalOutput };
