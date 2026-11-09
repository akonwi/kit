// Which agent's transcript the session page shows, Main's or a subagent's,
// and what the agent bar does with the subagent in view: its conversation,
// kept live while shown, its model and thinking, and dismissing it. The
// roster in each snapshot is authoritative; changes refresh it rather than
// projecting values of their own.
import {
  type Accessor,
  createEffect,
  createMemo,
  createSignal,
  onCleanup,
  type Setter,
  untrack,
} from "solid-js";
import { type Api, ApiError } from "../api/client";
import type { ModelCapability, SubagentConversation } from "../api/types";
import type { SessionState } from "../session/projection";
import { type ChildView, type ChildWatch, watchChild } from "./child";
import { dismissLocked, hasOutstandingWork, modelLocked, stripAgents, thinkingLocked } from "./roster";

export const GONE = "This subagent conversation is no longer available.";

/** What the last change or dismissal of the agent in view came to, when it needs saying. */
export interface Notice {
  text: string;
  tone: "danger" | "info";
  /** The conversation it's about; it shows only while that agent does. */
  agent: string;
}

interface Child {
  watch: ChildWatch;
  view: Accessor<ChildView>;
}

export interface Agents {
  /** The started agents, in the strip's order. */
  strip: Accessor<SubagentConversation[]>;
  /** The agent in view, or undefined for Main. */
  focused: Accessor<SubagentConversation | undefined>;
  /** Agents shown this visit, so each keeps its place when the reader comes back. */
  visited: Accessor<SubagentConversation[]>;
  /** Shows an agent's transcript, or Main's for undefined. */
  show(name: string | undefined): void;
  /** Whether a call naming this agent can open it. */
  has(name: string): boolean;
  /** The conversation's view and watch, made when first asked for. */
  child(agent: SubagentConversation): Child;
  models: Accessor<ModelCapability[] | undefined>;
  modelsError: Accessor<string>;
  loadModels(): Promise<void>;
  /** Why the agent's model, thinking, or dismissal can't happen now. */
  modelLocked(agent: SubagentConversation): string | undefined;
  thinkingLocked(agent: SubagentConversation): string | undefined;
  dismissLocked(agent: SubagentConversation): string | undefined;
  configure(agent: SubagentConversation, change: { model?: string; thinkingLevel?: string }): Promise<void>;
  dismiss(agent: SubagentConversation): Promise<boolean>;
  notice: Accessor<Notice | undefined>;
  clearNotice(): void;
  /** Hands a failure to the app first; true when the app took it. */
  fail(error: unknown): boolean;
}

export interface AgentsOptions {
  api: Api;
  session: string;
  state: Accessor<SessionState | undefined>;
  /** Whether the session's stream is live; nothing changes without it. */
  live: Accessor<boolean>;
  /** Whether the transcript area shows, not Changes, a file, or the scratchpad. */
  showing: Accessor<boolean>;
  /** Hands a failure to the app first; true when the app took it. */
  fail(error: unknown): boolean;
  /** Reads the session again, so the roster says what changed. */
  refresh(): void;
}

function message(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function createAgents(options: AgentsOptions): Agents {
  const [focus, setFocus] = createSignal<string>();
  const [visitedNames, setVisitedNames] = createSignal<string[]>([]);
  const [models, setModels] = createSignal<ModelCapability[]>();
  const [modelsError, setModelsError] = createSignal("");
  // One change at a time, for any agent, so each answer is about the request that asked.
  const [pending, setPending] = createSignal(false);
  const [dismissing, setDismissing] = createSignal(false);
  const [notice, setNotice] = createSignal<Notice>();
  const children = new Map<string, Child>();

  const strip = createMemo(
    () => stripAgents(options.state()?.subagents ?? [], options.state()?.rows ?? []),
    [],
    {
      equals: (a, b) =>
        a.length === b.length &&
        a.every(
          (agent, index) =>
            agent.id === b[index]?.id &&
            agent.state === b[index]?.state &&
            agent.updatedAt === b[index]?.updatedAt &&
            agent.model === b[index]?.model &&
            agent.thinkingLevel === b[index]?.thinkingLevel &&
            agent.generation === b[index]?.generation &&
            agent.queuedTasks === b[index]?.queuedTasks &&
            agent.activeTaskId === b[index]?.activeTaskId,
        ),
    },
  );
  const byName = (name: string | undefined) =>
    name === undefined ? undefined : strip().find((agent) => agent.agentName === name);
  const focused = createMemo(() => byName(focus()));
  const visited = createMemo(() =>
    visitedNames()
      .map(byName)
      .filter((agent): agent is SubagentConversation => agent !== undefined),
  );

  // An agent that leaves the roster, such as one dismissed elsewhere, leaves
  // the strip; Main comes back into view if it was showing. A conversation
  // that's gone takes its transcript with it.
  createEffect(() => {
    const current = strip();
    const names = new Set(current.map((agent) => agent.agentName));
    if (untrack(focus) !== undefined && !names.has(untrack(focus) as string)) setFocus(undefined);
    setVisitedNames((list) =>
      list.every((name) => names.has(name)) ? list : list.filter((name) => names.has(name)),
    );
    const ids = new Set(current.map((agent) => agent.id));
    for (const [id, child] of children) {
      if (ids.has(id)) continue;
      child.watch.stop();
      children.delete(id);
    }
  });

  // Only the conversation in view is asked about, and only while the
  // transcript area shows; a change to its agent asks at once.
  createEffect(() => {
    const agent = focused();
    const shown = options.showing();
    for (const [id, child] of children) child.watch.setActive(shown && id === agent?.id);
  });
  createEffect(() => {
    const agent = focused();
    if (agent) children.get(agent.id)?.watch.wake();
  });

  onCleanup(() => {
    for (const child of children.values()) child.watch.stop();
    children.clear();
  });

  const child = (agent: SubagentConversation): Child => {
    const existing = children.get(agent.id);
    if (existing) return existing;
    const [view, setView] = createSignal<ChildView>({ unavailable: false });
    const watch = watchChild(options.api, options.session, agent.id, {
      view: setView as Setter<ChildView>,
      fail: options.fail,
    });
    const made = { watch, view };
    children.set(agent.id, made);
    watch.setActive(options.showing() && focused()?.id === agent.id);
    return made;
  };

  const loadModels = async () => {
    if (models()) return;
    setModelsError("");
    try {
      setModels((await options.api.listModels()).models);
    } catch (failure) {
      if (!options.fail(failure)) setModelsError(message(failure, "Models couldn’t be loaded."));
    }
  };

  const lockedModel = (agent: SubagentConversation) => modelLocked(agent, options.live(), pending());
  const lockedThinking = (agent: SubagentConversation) => thinkingLocked(agent, options.live(), pending());
  const lockedDismiss = (agent: SubagentConversation) => dismissLocked(agent, options.live(), dismissing());

  return {
    strip,
    focused,
    visited,
    show(name) {
      if (name !== undefined && !byName(name)) return;
      // A failed change or dismissal belongs to the agent it was for.
      if (name !== focus() && !dismissing() && !pending()) setNotice(undefined);
      setFocus(name);
      if (name !== undefined && !visitedNames().includes(name)) setVisitedNames((list) => [...list, name]);
    },
    has: (name) => byName(name) !== undefined,
    child,
    models,
    modelsError,
    loadModels,
    modelLocked: lockedModel,
    thinkingLocked: lockedThinking,
    dismissLocked: lockedDismiss,
    async configure(agent, change) {
      const locked = change.model !== undefined ? lockedModel(agent) : lockedThinking(agent);
      if (locked || (change.model === undefined && change.thinkingLevel === undefined)) return;
      setPending(true);
      setNotice(undefined);
      try {
        const result = await options.api.configureSubagent(options.session, agent.id, {
          generation: agent.generation,
          ...change,
        });
        if (result.warnings?.length)
          setNotice({ text: result.warnings.join("\n"), tone: "info", agent: agent.id });
      } catch (failure) {
        if (options.fail(failure)) return;
        if (failure instanceof ApiError && failure.status === 409) {
          // The refreshed roster says whether work arrived in the meantime.
          setNotice({
            text:
              change.model !== undefined && hasOutstandingWork(agent)
                ? `${agent.agentName} has active or queued work. Change its model when that work finishes.`
                : "This conversation changed. Review its current configuration and try again.",
            tone: "danger",
            agent: agent.id,
          });
        } else if (failure instanceof ApiError && failure.status === 404) {
          setNotice({ text: GONE, tone: "danger", agent: agent.id });
        } else {
          setNotice({ text: message(failure, "That change didn’t apply."), tone: "danger", agent: agent.id });
        }
      } finally {
        setPending(false);
        // Success and failure both read the roster again; it says what applied.
        options.refresh();
      }
    },
    async dismiss(agent) {
      if (lockedDismiss(agent)) return false;
      setDismissing(true);
      setNotice(undefined);
      try {
        const result = await options.api.operateSubagent(options.session, {
          action: "dismiss",
          conversationId: agent.id,
          generation: agent.generation,
        });
        if (!result.dismissed) throw new Error("The server didn’t dismiss it.");
        // Only this conversation goes; one started again under the name stays.
        setVisitedNames((list) => list.filter((name) => name !== agent.agentName));
        if (focus() === agent.agentName) setFocus(undefined);
        return true;
      } catch (failure) {
        if (options.fail(failure)) return false;
        const reason =
          failure instanceof ApiError && failure.status === 409
            ? "This conversation changed. Review its current state and try again."
            : message(failure, "It wasn’t dismissed.");
        setNotice({ text: `Couldn’t dismiss. ${reason}`, tone: "danger", agent: agent.id });
        return false;
      } finally {
        setDismissing(false);
        options.refresh();
      }
    },
    notice: () => {
      const value = notice();
      return value && value.agent === focused()?.id ? value : undefined;
    },
    clearNotice: () => setNotice(undefined),
    fail: options.fail,
  };
}
