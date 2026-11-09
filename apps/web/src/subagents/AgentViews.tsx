// A subagent's side of the session page: its transcript, kept in place while
// another agent shows, and the bar that stands in for the composer.
import { createEffect, createMemo, createSignal, For, Match, on, onMount, Show, Switch } from "solid-js";
import type { ModelCapability, SubagentConversation } from "../api/types";
import { Activity } from "../session/Composer";
import { ModelChoices, ThinkingChoices } from "../session/ModelChoices";
import { TranscriptRow } from "../session/Transcript";
import { Icon, Spinner } from "../ui/icons";
import { Modal } from "../ui/Modal";
import type { Agents } from "./agents";
import { UserCaptionContext } from "./context";
import { agentLabel, ending, modelName, thinkingName } from "./roster";

const TASK_CAPTION = "Task from the main agent";

/** One agent's transcript; hidden, not removed, while another shows, so it keeps its place. */
export function AgentTranscript(props: { agents: Agents; agent: SubagentConversation; hidden: boolean }) {
  const child = props.agents.child(props.agent);
  const view = child.view;
  const rows = () => view().state?.rows ?? [];
  const [loadingOlder, setLoadingOlder] = createSignal(false);
  const [olderError, setOlderError] = createSignal("");
  let scroller: HTMLDivElement | undefined;
  let stick = true;

  const atBottom = () => !scroller || scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
  // Follow new rows while the reader is at the bottom, as Main's transcript does.
  createEffect(
    on(rows, () => {
      if (!stick || !scroller) return;
      requestAnimationFrame(() => scroller?.scrollTo({ top: scroller.scrollHeight }));
    }),
  );

  const loadOlder = async () => {
    if (!scroller || loadingOlder()) return;
    setLoadingOlder(true);
    setOlderError("");
    const before = scroller.scrollHeight - scroller.scrollTop;
    try {
      await child.watch.loadOlder();
      requestAnimationFrame(() => {
        if (scroller) scroller.scrollTop = scroller.scrollHeight - before;
      });
    } catch (failure) {
      if (!props.agents.fail(failure))
        setOlderError(failure instanceof Error ? failure.message : "Earlier messages couldn’t be read.");
    } finally {
      setLoadingOlder(false);
    }
  };

  const tryAgain = () => {
    if (view().halted) child.watch.retry();
    else child.watch.wake();
  };

  return (
    <div
      class="transcript"
      data-agent={props.agent.agentName}
      hidden={props.hidden}
      ref={scroller}
      onScroll={() => {
        stick = atBottom();
      }}
    >
      <Switch>
        {/* A conversation that's gone shows no stale copy of itself. */}
        <Match when={view().unavailable}>
          <p class="empty">{view().error}</p>
        </Match>
        <Match when={view().state}>
          {(state) => (
            <UserCaptionContext.Provider value={TASK_CAPTION}>
              <Show when={state().hasMoreMessages}>
                <button type="button" class="load-older" disabled={loadingOlder()} onClick={loadOlder}>
                  {loadingOlder() ? "Loading…" : "Show earlier messages"}
                </button>
                <Show when={olderError()}>
                  <p class="error small center">{olderError()}</p>
                </Show>
              </Show>
              <Show when={rows().length > 0} fallback={<p class="empty">No messages yet.</p>}>
                <For each={rows()}>
                  {(row, index) => <TranscriptRow row={row} last={index() === rows().length - 1} />}
                </For>
              </Show>
            </UserCaptionContext.Provider>
          )}
        </Match>
        <Match when={view().error}>
          {(error) => (
            <div class="empty-state agent-load-error" role="alert">
              <p class="error">{error()}</p>
              <button type="button" onClick={tryAgain}>
                Try again
              </button>
            </div>
          )}
        </Match>
        <Match when={true}>
          <p class="agent-loading muted">
            <Spinner size={16} />
            <span>Loading conversation…</span>
          </p>
        </Match>
      </Switch>
    </div>
  );
}

/** What stands in for the composer while a subagent shows: what it's doing, and its bar. */
export function AgentDock(props: { agents: Agents; agent: SubagentConversation }) {
  const view = () => props.agents.child(props.agent).view();
  const working = () => Boolean(view().state?.activeTurnId);
  const [confirming, setConfirming] = createSignal(false);
  // The bar names the model, which the catalog has.
  onMount(() => void props.agents.loadModels());
  // Live updates that stopped after the transcript loaded say so above the bar.
  const liveError = () => (view().state && !view().unavailable ? view().error : undefined);
  return (
    <section class="agent-dock" aria-label={`${props.agent.agentName} conversation`}>
      <Show when={props.agents.notice()}>
        {(notice) => (
          <div class="activity agent-notice" data-tone={notice().tone} role="alert">
            <Icon name={notice().tone === "danger" ? "alertCircle" : "info"} size={16} />
            <span class="grow">{notice().text}</span>
            <button
              type="button"
              class="icon-button small"
              aria-label="Dismiss notice"
              onClick={() => props.agents.clearNotice()}
            >
              <Icon name="close" size={16} />
            </button>
          </div>
        )}
      </Show>
      <Switch>
        <Match when={liveError()}>
          {(error) => (
            <div class="activity agent-notice" data-tone="danger" role="alert">
              <Icon name="alertCircle" size={16} />
              <span class="grow">{error()}</span>
              <Show when={view().halted}>
                <button
                  type="button"
                  class="link"
                  onClick={() => props.agents.child(props.agent).watch.retry()}
                >
                  Try again
                </button>
              </Show>
            </div>
          )}
        </Match>
        <Match when={working() && view().state}>{(state) => <Activity session={state()} />}</Match>
        <Match when={ending(props.agent)}>
          {(end) => (
            <div class="activity agent-notice" data-tone={end().tone}>
              <Icon name={props.agent.state === "aborted" ? "minusCircle" : "alertCircle"} size={16} />
              <span class="grow">{end().text}</span>
            </div>
          )}
        </Match>
      </Switch>
      <div class="agent-bar">
        <span class="grow">
          <span class="agent-bar-label">{agentLabel(props.agent, props.agents.models())}</span>
          <span class="muted small">Its results go to Main</span>
        </span>
        <AgentMenu agents={props.agents} agent={props.agent} onDismiss={() => setConfirming(true)} />
      </div>
      <DismissDialog
        agents={props.agents}
        agent={props.agent}
        open={confirming()}
        onClose={() => setConfirming(false)}
      />
    </section>
  );
}

function AgentMenu(props: { agents: Agents; agent: SubagentConversation; onDismiss(): void }) {
  const [view, setView] = createSignal<"main" | "model" | "thinking">("main");
  let popover: HTMLDivElement | undefined;
  const current = createMemo(() => props.agents.models()?.find((model) => model.id === props.agent.model));
  const levels = () => current()?.thinkingLevels ?? [];
  const modelLock = () => props.agents.modelLocked(props.agent);
  const thinkingLock = () => props.agents.thinkingLocked(props.agent);
  const dismissLock = () => props.agents.dismissLocked(props.agent);
  const id = () => `agent-menu-${props.agent.id}`;

  const close = () => popover?.hidePopover();
  const chooseModel = (model: ModelCapability) => {
    if (model.id === props.agent.model) return close();
    const level = model.thinkingLevels.includes(props.agent.thinkingLevel)
      ? props.agent.thinkingLevel
      : (model.thinkingLevels[0] ?? "off");
    close();
    void props.agents.configure(props.agent, { model: model.id, thinkingLevel: level });
  };
  const chooseThinking = (level: string) => {
    close();
    if (level !== props.agent.thinkingLevel)
      void props.agents.configure(props.agent, { thinkingLevel: level });
  };

  return (
    <>
      <button
        type="button"
        class="icon-button agent-menu-button"
        popovertarget={id()}
        aria-label={`${props.agent.agentName} menu`}
      >
        <Icon name="more" />
      </button>
      <div
        id={id()}
        popover
        class="menu sheet-menu agent-menu"
        ref={popover}
        onToggle={(event) => {
          if ((event as ToggleEvent).newState === "open") {
            setView("main");
            void props.agents.loadModels();
          }
        }}
      >
        <Switch>
          <Match when={view() === "main"}>
            <button
              type="button"
              class="menu-row"
              disabled={Boolean(modelLock())}
              onClick={() => setView("model")}
            >
              <span class="grow">
                <span>Model</span>
                <span class="muted small">{modelName(props.agent.model, props.agents.models())}</span>
                <Show when={modelLock()}>{(reason) => <span class="muted small">{reason()}</span>}</Show>
              </span>
              <Show when={!modelLock()}>
                <Icon name="chevRight" size={16} />
              </Show>
            </button>
            <button
              type="button"
              class="menu-row"
              disabled={Boolean(thinkingLock()) || levels().length === 0}
              onClick={() => setView("thinking")}
            >
              <span class="grow">
                <span>Thinking</span>
                <span class="muted small">{thinkingName(props.agent.thinkingLevel)}</span>
                <Show when={thinkingLock()}>{(reason) => <span class="muted small">{reason()}</span>}</Show>
              </span>
              <Show when={!thinkingLock()}>
                <Icon name="chevRight" size={16} />
              </Show>
            </button>
            <hr />
            <button
              type="button"
              class="menu-action"
              data-variant="danger"
              disabled={Boolean(dismissLock())}
              onClick={() => {
                close();
                props.onDismiss();
              }}
            >
              <Icon name="trash" size={18} />
              <span class="grow">
                <span>Dismiss {props.agent.agentName}…</span>
                <Show when={dismissLock()}>{(reason) => <span class="muted small">{reason()}</span>}</Show>
              </span>
            </button>
          </Match>
          <Match when={view() === "model"}>
            <button type="button" class="menu-back" onClick={() => setView("main")}>
              <Icon name="back" size={16} />
              <strong>Model</strong>
            </button>
            <hr />
            <Show
              when={props.agents.models()}
              fallback={
                <p class="muted small menu-note">{props.agents.modelsError() || "Loading models…"}</p>
              }
            >
              {(list) => (
                <ModelChoices
                  models={list()}
                  current={props.agent.model}
                  disabled={(model) => Boolean(modelLock()) || !model.available}
                  onChoose={chooseModel}
                />
              )}
            </Show>
          </Match>
          <Match when={view() === "thinking"}>
            <button type="button" class="menu-back" onClick={() => setView("main")}>
              <Icon name="back" size={16} />
              <strong>Thinking</strong>
            </button>
            <hr />
            <ThinkingChoices
              levels={levels()}
              current={props.agent.thinkingLevel}
              disabled={Boolean(thinkingLock())}
              onChoose={chooseThinking}
            />
          </Match>
        </Switch>
      </div>
    </>
  );
}

function DismissDialog(props: {
  agents: Agents;
  agent: SubagentConversation;
  open: boolean;
  onClose(): void;
}) {
  const [pending, setPending] = createSignal(false);
  // A failure shows above the bar, where the agent still is.
  const dismiss = async () => {
    if (pending()) return;
    setPending(true);
    try {
      await props.agents.dismiss(props.agent);
    } finally {
      setPending(false);
      props.onClose();
    }
  };
  return (
    <Modal
      open={props.open}
      onClose={props.onClose}
      alert
      title={`Dismiss “${props.agent.agentName}”?`}
      footer={
        <>
          <button type="button" disabled={pending()} onClick={props.onClose}>
            Cancel
          </button>
          <button type="button" data-variant="danger" disabled={pending()} onClick={dismiss}>
            {pending() ? "Dismissing…" : "Dismiss"}
          </button>
        </>
      }
    >
      <p class="muted">Its active and queued work will be aborted. This can’t be undone.</p>
    </Modal>
  );
}
