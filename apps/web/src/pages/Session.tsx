// One session: its transcript, kept live, and the composer or the request
// the run is waiting on.
import {
  createEffect,
  createMemo,
  createSignal,
  For,
  Match,
  on,
  onCleanup,
  onMount,
  Show,
  Switch,
} from "solid-js";
import { ApiError } from "../api/client";
import type { ModelCapability } from "../api/types";
import { useKit, useReady } from "../app/kit";
import { back } from "../app/router";
import { createHighlighter, HighlightContext } from "../markdown/highlight";
import { AttachmentsContext, AttachmentViewer, type Openable } from "../session/AttachmentViews";
import { Draft } from "../session/attachments";
import { Composer } from "../session/Composer";
import { InteractionDock } from "../session/InteractionDock";
import { AttachmentLibrary } from "../session/library";
import {
  type CompactionOutcome,
  compactionOutcome,
  compactUnavailable,
  forkUnavailable,
  newId,
} from "../session/manage";
import { providerName, THINKING_LABELS } from "../session/models";
import type { SessionState } from "../session/projection";
import {
  DeleteDialog,
  DetailsSheet,
  FORK_FAILURE_KEY,
  ForkDialog,
  RenameDialog,
  type SessionDialog,
} from "../session/SessionActions";
import { createShell, placeShellRuns } from "../session/shell";
import { TranscriptRow } from "../session/Transcript";
import { type Connection, type Watch, watchSession } from "../session/watch";
import { displayPath, serverUserHome, tokens } from "../ui/format";
import { Icon } from "../ui/icons";

/** How long a page stays hidden before it replaces its event stream. */
const RESUME_AFTER_MS = 2_000;

export function SessionPage(props: { id: string }) {
  const kit = useKit();
  const ready = useReady();
  const home = serverUserHome(ready.health.home);
  const [state, setState] = createSignal<SessionState | undefined>();
  const [connection, setConnection] = createSignal<Connection>("connecting");
  const [missing, setMissing] = createSignal(false);
  const [loadingOlder, setLoadingOlder] = createSignal(false);
  const [dialog, setDialog] = createSignal<SessionDialog>();
  const [modelName, setModelName] = createSignal<string>();
  const [compactionPending, setCompactionPending] = createSignal(false);
  const [outcome, setOutcome] = createSignal<CompactionOutcome>();
  let scroller: HTMLDivElement | undefined;

  // A fork whose first message didn't start leaves it for this composer.
  const forkFailure = (() => {
    const key = FORK_FAILURE_KEY + props.id;
    const saved = sessionStorage.getItem(key);
    sessionStorage.removeItem(key);
    try {
      const value = saved ? (JSON.parse(saved) as { text?: unknown; message?: unknown }) : undefined;
      if (typeof value?.text === "string" && typeof value.message === "string")
        return { text: value.text, error: `The first message didn’t start. ${value.message}` };
    } catch {
      // A malformed note is no note.
    }
    return undefined;
  })();
  let stick = true;

  // The message being written and the server's attachments belong to the
  // page, so a request that takes the composer's place loses neither.
  const draft = new Draft(
    ready.api,
    props.id,
    {
      imageRejectingModel() {
        const info = state()?.info;
        if (!info?.inputs || info.inputs.includes("image")) return undefined;
        return modelName() ?? info.model;
      },
      fail: kit.fail,
    },
    forkFailure?.text,
  );
  const library = new AttachmentLibrary(ready.api, props.id);
  // Code in replies takes the server's colors, asked for a reply at a time.
  const highlighter = createHighlighter((blocks, signal) => ready.api.highlightCode(blocks, signal));
  const [viewing, setViewing] = createSignal<Openable>();
  onCleanup(() => {
    draft.dispose();
    library.dispose();
  });

  // This device's shell commands, shown among the transcript's rows until
  // the transcript records them; a `!!` command never is.
  const shell = createShell(
    ready.api,
    () => props.id,
    (error) => kit.fail(error),
  );
  const rows = createMemo(() => placeShellRuns(state()?.rows ?? [], shell.runs(), shell.anchors()));
  // A command the transcript has recorded has finished, whatever the last poll said.
  createEffect(() => {
    for (const row of state()?.rows ?? []) if (row.kind === "shell") shell.settle(row.run);
  });
  // A command already running when the page loaded, from any device.
  createEffect(() => {
    const id = state()?.activeShellId;
    if (id) shell.follow(id);
  });

  const watch: Watch = watchSession(ready.api, props.id, {
    state: setState,
    connection: setConnection,
    fatal(error) {
      if (kit.fail(error)) return;
      if (error instanceof ApiError && error.status === 404) setMissing(true);
      else setConnection("reconnecting");
    },
  });

  // A browser can suspend a hidden page and leave its event stream open but
  // silent, so a page that comes back takes a fresh snapshot and stream.
  let hiddenAt: number | undefined;
  const visibility = () => {
    if (document.visibilityState === "hidden") {
      hiddenAt = Date.now();
      return;
    }
    const away = hiddenAt === undefined ? 0 : Date.now() - hiddenAt;
    hiddenAt = undefined;
    if (away >= RESUME_AFTER_MS) watch.resume();
    else watch.wake();
  };
  const resume = () => watch.resume();
  onMount(() => {
    document.addEventListener("visibilitychange", visibility);
    // Chrome fires resume when it thaws a page it froze in the background.
    document.addEventListener("resume", resume);
    window.addEventListener("online", resume);
  });
  onCleanup(() => {
    watch.stop();
    document.removeEventListener("visibilitychange", visibility);
    document.removeEventListener("resume", resume);
    window.removeEventListener("online", resume);
    document.title = "Kit";
  });

  createEffect(() => {
    const name = state()?.info.name;
    document.title = name ? `${name} · Kit` : "Kit";
  });

  // Follow new rows while the reader is at the bottom.
  const atBottom = () => !scroller || scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
  createEffect(
    on(rows, () => {
      if (!stick || !scroller) return;
      requestAnimationFrame(() => scroller?.scrollTo({ top: scroller.scrollHeight }));
    }),
  );

  const loadOlder = async () => {
    if (!scroller || loadingOlder()) return;
    setLoadingOlder(true);
    const before = scroller.scrollHeight - scroller.scrollTop;
    try {
      await watch.loadOlder();
      requestAnimationFrame(() => {
        if (scroller) scroller.scrollTop = scroller.scrollHeight - before;
      });
    } catch (failure) {
      kit.fail(failure);
    } finally {
      setLoadingOlder(false);
    }
  };

  // A retry resolves the same compaction rather than starting another, as the
  // Mac app does, until the server answers for it.
  let compactionId: string | undefined;
  let outcomeTimer: ReturnType<typeof setTimeout> | undefined;
  const showOutcome = (value: CompactionOutcome | undefined) => {
    clearTimeout(outcomeTimer);
    setOutcome(value);
    if (value?.tone === "info") outcomeTimer = setTimeout(() => setOutcome(undefined), 5_000);
  };
  onCleanup(() => clearTimeout(outcomeTimer));
  const compact = async () => {
    if (compactionPending()) return;
    setCompactionPending(true);
    showOutcome(undefined);
    compactionId ??= newId("compact");
    try {
      const result = await ready.api.compactSession(props.id, compactionId);
      compactionId = undefined;
      // A compaction that ran shows as its marker in the transcript.
      if (!result.compacted) showOutcome(compactionOutcome(result));
    } catch (failure) {
      if (kit.fail(failure)) return;
      const value = compactionOutcome(undefined, failure);
      if (value.tone === "error" && !value.retry) compactionId = undefined;
      showOutcome(value);
    } finally {
      setCompactionPending(false);
      // The transcript changed under the stream; take it again.
      watch.resume();
    }
  };

  const interaction = createMemo(() => state()?.pendingInteractions[0]);
  const interactionId = createMemo(() => interaction()?.id);

  return (
    <AttachmentsContext.Provider value={{ library, open: setViewing }}>
      <HighlightContext.Provider value={highlighter}>
        <div class="screen session-screen">
          <header class="bar">
            <button type="button" class="icon-button" aria-label="Back" onClick={back}>
              <Icon name="back" />
            </button>
            <div class="bar-title">
              <h1>{state()?.info.name || (state() ? "Untitled session" : "")}</h1>
              <p class="mono muted">{state() ? displayPath(state()?.info.cwd ?? "", home) : ""}</p>
            </div>
            <Show when={state()}>
              {(current) => (
                <SessionMenu
                  session={current()}
                  compactionPending={compactionPending()}
                  onCompact={compact}
                  onModelName={setModelName}
                  onOpen={setDialog}
                />
              )}
            </Show>
          </header>
          <Switch>
            <Match when={missing()}>
              <div class="empty-state">
                <p>This session isn’t on the server anymore.</p>
                <button type="button" onClick={back}>
                  Back to sessions
                </button>
              </div>
            </Match>
            <Match when={state()}>
              {(current) => (
                <>
                  <div
                    class="transcript"
                    ref={scroller}
                    onScroll={() => {
                      stick = atBottom();
                    }}
                  >
                    <Show when={current().hasMoreMessages}>
                      <button type="button" class="load-older" disabled={loadingOlder()} onClick={loadOlder}>
                        {loadingOlder() ? "Loading…" : "Show earlier messages"}
                      </button>
                    </Show>
                    <Show when={rows().length > 0} fallback={<p class="empty">No messages yet.</p>}>
                      <For each={rows()}>
                        {(row, index) => <TranscriptRow row={row} last={index() === rows().length - 1} />}
                      </For>
                    </Show>
                  </div>
                  <Show when={outcome()}>
                    {(value) => (
                      <div
                        class="feedback"
                        data-tone={value().tone}
                        role={value().tone === "error" ? "alert" : "status"}
                      >
                        <Icon name={value().tone === "error" ? "alertCircle" : "info"} size={16} />
                        <span class="grow">
                          <strong>{value().title}</strong>
                          <Show when={"detail" in value() && (value() as { detail: string }).detail}>
                            {(detail) => <span class="muted small">{detail()}</span>}
                          </Show>
                        </span>
                        <Show when={"retry" in value() && (value() as { retry: boolean }).retry}>
                          <button type="button" data-size="small" onClick={compact}>
                            Retry
                          </button>
                        </Show>
                        <button
                          type="button"
                          class="icon-button small"
                          aria-label="Dismiss"
                          onClick={() => showOutcome(undefined)}
                        >
                          <Icon name="close" size={16} />
                        </button>
                      </div>
                    )}
                  </Show>
                  <RenameDialog
                    api={ready.api}
                    session={current()}
                    open={dialog() === "rename"}
                    onClose={() => setDialog(undefined)}
                  />
                  <ForkDialog
                    api={ready.api}
                    session={current()}
                    open={dialog() === "fork"}
                    onClose={() => setDialog(undefined)}
                  />
                  <DetailsSheet
                    api={ready.api}
                    session={current()}
                    home={home}
                    modelName={modelName()}
                    open={dialog() === "details"}
                    onClose={() => setDialog(undefined)}
                  />
                  <DeleteDialog
                    api={ready.api}
                    session={current()}
                    open={dialog() === "delete"}
                    onClose={() => setDialog(undefined)}
                  />
                  {/* Keyed by id: each request gets a fresh dock, with no answer or
                  busy state carried over from the one before. */}
                  <Show
                    when={interactionId()}
                    keyed
                    fallback={
                      <Composer
                        api={ready.api}
                        session={current()}
                        connection={connection()}
                        fail={kit.fail}
                        reconnect={() => watch.wake()}
                        draft={draft}
                        initialError={forkFailure?.error}
                        shell={shell}
                        anchor={() => current().rows.at(-1)?.id ?? ""}
                      />
                    }
                  >
                    {(_id) => (
                      <Show when={interaction()}>
                        {(request) => (
                          <InteractionDock
                            api={ready.api}
                            sessionId={props.id}
                            interaction={request()}
                            fail={kit.fail}
                          />
                        )}
                      </Show>
                    )}
                  </Show>
                </>
              )}
            </Match>
            <Match when={true}>
              <p class="empty">Loading…</p>
            </Match>
          </Switch>
          <AttachmentViewer attachment={viewing()} onClose={() => setViewing(undefined)} />
        </div>
      </HighlightContext.Provider>
    </AttachmentsContext.Provider>
  );
}

interface SessionMenuProps {
  session: SessionState;
  compactionPending: boolean;
  onCompact(): void;
  /** Reports the session model's display name once the catalog loads. */
  onModelName(name: string | undefined): void;
  onOpen(dialog: SessionDialog): void;
}

function SessionMenu(props: SessionMenuProps) {
  const kit = useKit();
  const ready = useReady();
  const [view, setView] = createSignal<"main" | "model" | "thinking">("main");
  const [models, setModels] = createSignal<ModelCapability[]>();
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  let popover: HTMLDivElement | undefined;

  const loadModels = async () => {
    if (models()) return;
    try {
      setModels((await ready.api.listModels()).models);
    } catch (failure) {
      if (!kit.fail(failure))
        setError(failure instanceof Error ? failure.message : "Models couldn’t be loaded.");
    }
  };

  const current = createMemo(() => models()?.find((model) => model.id === props.session.info.model));
  createEffect(() => props.onModelName(current()?.name));
  const compactReason = () => compactUnavailable(props.session, props.compactionPending);
  const forkReason = () => forkUnavailable(props.session.info);
  const act = (run: () => void) => {
    popover?.hidePopover();
    run();
  };
  const levels = () => current()?.thinkingLevels ?? [];
  const groups = createMemo(() => {
    const byProvider = new Map<string, ModelCapability[]>();
    for (const model of models() ?? []) {
      if (!model.available && model.id !== props.session.info.model) continue;
      byProvider.set(model.provider, [...(byProvider.get(model.provider) ?? []), model]);
    }
    return [...byProvider.entries()];
  });

  const configure = async (model: string, thinkingLevel: string | undefined) => {
    setBusy(true);
    setError("");
    try {
      await ready.api.configureSession(props.session.info.id, {
        expectedRevision: props.session.info.configurationRevision,
        model,
        thinkingLevel,
      });
      popover?.hidePopover();
    } catch (failure) {
      if (kit.fail(failure)) return;
      if (failure instanceof ApiError && failure.status === 409)
        setError("The session is busy. Try again when the turn ends.");
      else setError(failure instanceof Error ? failure.message : "That change didn’t apply.");
    } finally {
      setBusy(false);
    }
  };

  const chooseModel = (model: ModelCapability) => {
    const level = model.thinkingLevels.includes(props.session.info.thinkingLevel)
      ? props.session.info.thinkingLevel
      : (model.thinkingLevels[0] ?? "off");
    void configure(model.id, level);
  };

  const percent = () => {
    const used = props.session.contextTokens;
    const window = props.session.contextWindow;
    return used !== undefined && window ? Math.min(100, Math.round((used / window) * 100)) : undefined;
  };

  return (
    <>
      <button type="button" class="icon-button" popovertarget="session-menu" aria-label="Session menu">
        <Icon name="more" />
      </button>
      <div
        id="session-menu"
        popover
        class="menu sheet-menu"
        ref={popover}
        onToggle={(event) => {
          if ((event as ToggleEvent).newState === "open") {
            setView("main");
            setError("");
            void loadModels();
          }
        }}
      >
        <Switch>
          <Match when={view() === "main"}>
            <Show when={percent() !== undefined}>
              <div class="menu-section context">
                <span class="line">
                  <span class="grow">Context</span>
                  <span class="mono muted small">
                    {tokens(props.session.contextTokens ?? 0)} / {tokens(props.session.contextWindow ?? 0)} ·{" "}
                    {percent()}%
                  </span>
                </span>
                <meter min={0} max={100} value={percent()} aria-label="Context used" />
              </div>
              <hr />
            </Show>
            <button type="button" class="menu-row" onClick={() => setView("model")}>
              <span class="grow">
                <span>Model</span>
                <span class="muted small">{current()?.name ?? props.session.info.model}</span>
              </span>
              <Icon name="chevRight" size={16} />
            </button>
            <button
              type="button"
              class="menu-row"
              disabled={levels().length === 0}
              onClick={() => setView("thinking")}
            >
              <span class="grow">
                <span>Thinking</span>
                <span class="muted small">
                  {THINKING_LABELS[props.session.info.thinkingLevel] ?? props.session.info.thinkingLevel}
                </span>
              </span>
              <Icon name="chevRight" size={16} />
            </button>
            <hr />
            <button
              type="button"
              class="menu-action"
              disabled={Boolean(compactReason())}
              onClick={() => act(props.onCompact)}
            >
              <Icon name="compact" size={18} />
              <span class="grow">
                <span>Compact context</span>
                <Show when={compactReason()}>
                  <span class="muted small">{compactReason()}</span>
                </Show>
              </span>
            </button>
            <button type="button" class="menu-action" onClick={() => act(() => props.onOpen("rename"))}>
              <Icon name="edit" size={18} />
              <span class="grow">Rename…</span>
            </button>
            <button
              type="button"
              class="menu-action"
              disabled={Boolean(forkReason())}
              onClick={() => act(() => props.onOpen("fork"))}
            >
              <Icon name="fork" size={18} />
              <span class="grow">
                <span>Fork…</span>
                <Show when={forkReason()}>
                  <span class="muted small">{forkReason()}</span>
                </Show>
              </span>
            </button>
            <button type="button" class="menu-action" onClick={() => act(() => props.onOpen("details"))}>
              <Icon name="info" size={18} />
              <span class="grow">Details</span>
            </button>
            <hr />
            <button
              type="button"
              class="menu-action"
              data-variant="danger"
              onClick={() => act(() => props.onOpen("delete"))}
            >
              <Icon name="trash" size={18} />
              <span class="grow">Delete…</span>
            </button>
          </Match>
          <Match when={view() === "model"}>
            <button type="button" class="menu-back" onClick={() => setView("main")}>
              <Icon name="back" size={16} />
              <strong>Model</strong>
            </button>
            <hr />
            <Show when={props.session.activeTurnId}>
              <p class="muted small menu-note">Model can change when the turn ends</p>
            </Show>
            <Show when={models()} fallback={<p class="muted small menu-note">Loading models…</p>}>
              <div class="menu-scroll">
                <For each={groups()}>
                  {([provider, list]) => (
                    <>
                      <span class="eyebrow menu-group">{providerName(provider)}</span>
                      <For each={list}>
                        {(model) => (
                          <button
                            type="button"
                            class="menu-row"
                            aria-current={model.id === props.session.info.model || undefined}
                            disabled={busy() || !model.available || Boolean(props.session.activeTurnId)}
                            onClick={() => chooseModel(model)}
                          >
                            <span class="grow">
                              <span>{model.name}</span>
                              <span class="mono muted small">{model.id.replace(/^[^/]+\//, "")}</span>
                            </span>
                            <Show when={model.id === props.session.info.model}>
                              <Icon name="check" size={16} class="accent" />
                            </Show>
                          </button>
                        )}
                      </For>
                    </>
                  )}
                </For>
              </div>
            </Show>
          </Match>
          <Match when={view() === "thinking"}>
            <button type="button" class="menu-back" onClick={() => setView("main")}>
              <Icon name="back" size={16} />
              <strong>Thinking</strong>
            </button>
            <hr />
            <For each={levels()}>
              {(level) => (
                <button
                  type="button"
                  class="menu-row"
                  aria-current={level === props.session.info.thinkingLevel || undefined}
                  disabled={busy()}
                  onClick={() => configure(props.session.info.model, level)}
                >
                  <span class="grow">{THINKING_LABELS[level] ?? level}</span>
                  <Show when={level === props.session.info.thinkingLevel}>
                    <Icon name="check" size={16} class="accent" />
                  </Show>
                </button>
              )}
            </For>
          </Match>
        </Switch>
        <Show when={error()}>
          <p class="error small menu-note" role="alert">
            {error()}
          </p>
        </Show>
      </div>
    </>
  );
}
