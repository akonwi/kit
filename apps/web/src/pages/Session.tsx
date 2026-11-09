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
  untrack,
} from "solid-js";
import { ApiError, isAbort } from "../api/client";
import type { ModelCapability } from "../api/types";
import { useKit, useReady } from "../app/kit";
import { back, hash, navigate } from "../app/router";
import { ChangesView, changesProblem, commentCount } from "../changes/Changes";
import { onWorkingTree } from "../changes/comments";
import { type ChangeSummary, filesLabel, summarize } from "../changes/diff";
import { FilesContext } from "../files/context";
import { FileView } from "../files/FileView";
import { type FileLocation, fileHash, folderPath, parseFileHash } from "../files/file";
import { OpenFileSheet } from "../files/OpenFile";
import { createHighlighter, HighlightContext } from "../markdown/highlight";
import { createPadHandle, ScratchpadView } from "../scratchpad/ScratchpadView";
import { AttachmentsContext, AttachmentViewer, type Openable } from "../session/AttachmentViews";
import { Draft } from "../session/attachments";
import { Composer } from "../session/Composer";
import { InteractionDock } from "../session/InteractionDock";
import { AttachmentLibrary } from "../session/library";
import { ModelChoices, ThinkingChoices } from "../session/ModelChoices";
import {
  type CompactionOutcome,
  compactionOutcome,
  compactUnavailable,
  forkUnavailable,
  newId,
} from "../session/manage";
import { THINKING_LABELS } from "../session/models";
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
import { AgentStrip } from "../subagents/AgentStrip";
import { AgentDock, AgentTranscript } from "../subagents/AgentViews";
import { createAgents } from "../subagents/agents";
import { AgentsContext } from "../subagents/context";
import { displayPath, serverUserHome, tokens } from "../ui/format";
import { Icon } from "../ui/icons";

/** The fragment that shows an agent's conversation. */
function agentHash(name: string): string {
  return `#agent=${encodeURIComponent(name)}`;
}

function parseAgentHash(value: string): string | undefined {
  if (!value.startsWith("#agent=")) return undefined;
  try {
    return decodeURIComponent(value.slice("#agent=".length)) || undefined;
  } catch {
    return undefined;
  }
}

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

  // The Changes screen is this page's #changes, so Back and reload keep it.
  const changesOpen = () => hash() === "#changes";
  const openChanges = () => {
    if (!changesOpen()) navigate(`/sessions/${props.id}#changes`, { state: { inApp: true, changes: true } });
  };
  const closeChanges = () => {
    if (history.state?.changes) history.back();
    else navigate(`/sessions/${props.id}${agentFragment()}`, { replace: true, state: { inApp: true } });
  };
  // The scratchpad is this page's #scratchpad. Its draft lives as long as the page.
  const scratchpad = createPadHandle(ready.api, props.id, kit.fail);
  const padOpen = () => hash() === "#scratchpad";
  const openPad = () => {
    if (!padOpen())
      navigate(`/sessions/${props.id}#scratchpad`, { state: { inApp: true, scratchpad: true } });
  };
  const closePad = () => {
    if (history.state?.scratchpad) history.back();
    else navigate(`/sessions/${props.id}${agentFragment()}`, { replace: true, state: { inApp: true } });
  };
  createEffect(() => {
    const record = state()?.scratchpad;
    if (record) scratchpad.pad.observe(record);
  });
  // Another screen covers the transcript and the bar above it.
  const covered = () => changesOpen() || Boolean(fileLocation()) || padOpen();
  // A file is this page's #file=…, so Back returns to where it was opened from.
  const fileLocation = createMemo(() => parseFileHash(hash()), undefined, {
    equals: (a, b) => a?.path === b?.path && a?.line === b?.line && a?.end === b?.end,
  });
  const [choosingFile, setChoosingFile] = createSignal(false);
  const openFile = (location: FileLocation) => {
    setChoosingFile(false);
    navigate(`/sessions/${props.id}${fileHash(location)}`, { state: { inApp: true, file: true } });
  };
  const closeFile = () => {
    if (history.state?.file) history.back();
    else navigate(`/sessions/${props.id}${agentFragment()}`, { replace: true, state: { inApp: true } });
  };
  const files = {
    resolve: (path: string) => {
      const cwd = state()?.workspace?.cwd ?? state()?.info.cwd;
      return cwd ? folderPath(path, cwd, home) : undefined;
    },
    open: (path: string, line?: number, end?: number) => openFile({ path, line, end }),
  };
  const refreshComments = () => watch.refreshComments();
  /**
   * Reads the comments again with the server's check of their source. The
   * server holds uncommitted changes for two minutes, so those are read
   * again first: unchanged files keep their comments.
   */
  const checkComments = async () => {
    const current = state();
    if (!current || current.comments.length === 0) return;
    const workspaceId = current.workspace?.workspaceId;
    if (workspaceId && current.comments.some(onWorkingTree)) {
      try {
        await ready.api.observeWorkingTree(props.id, { workspaceId, pageSize: 1 });
      } catch {
        // The list says which comments this leaves stale.
      }
    }
    await refreshComments();
  };

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
    if (away >= RESUME_AFTER_MS) {
      watch.resume();
      void checkComments().catch(() => {});
    } else watch.wake();
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

  // A snapshot's check of the comments may be on changes the server no
  // longer holds, so the first one is checked again.
  let checked = false;
  createEffect(() => {
    if (checked || !state()) return;
    checked = true;
    void checkComments().catch(() => {});
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

  // The agent in view, Main's or a subagent's; it's this page's #agent=…,
  // replaced rather than added, so Back still leaves the session.
  const agents = createAgents({
    api: ready.api,
    session: props.id,
    state,
    live: () => connection() === "live",
    showing: () => !covered(),
    fail: kit.fail,
    refresh: () => {
      void watch.refreshSubagents().catch((failure) => {
        if (!isAbort(failure)) kit.fail(failure);
      });
    },
  });
  const showAgent = (name: string | undefined) => {
    agents.show(name);
    const target = agents.focused()?.agentName;
    navigate(`/sessions/${props.id}${target ? agentHash(target) : ""}`, {
      replace: true,
      state: history.state,
    });
  };
  // The address says which agent shows: a reload or Back to an agent shows
  // it once the roster has it, and one with no agent shows Main. Another
  // screen's fragment says nothing about it.
  const loaded = createMemo(() => state() !== undefined);
  createEffect(() => {
    if (covered()) return;
    const name = parseAgentHash(hash());
    if (name === undefined) untrack(() => agents.show(undefined));
    else if (agents.has(name)) untrack(() => agents.show(name));
    else if (loaded()) navigate(`/sessions/${props.id}`, { replace: true, state: history.state });
  });
  // An agent that leaves, such as one dismissed, leaves the address too.
  createEffect(
    on(agents.focused, (agent, previous) => {
      if (!agent && previous && parseAgentHash(hash()) !== undefined)
        navigate(`/sessions/${props.id}`, { replace: true, state: history.state });
    }),
  );
  /** The fragment of the agent in view, to come back to it from another screen. */
  function agentFragment(): string {
    const name = agents.focused()?.agentName;
    return name ? agentHash(name) : "";
  }
  const agentInView = () => agents.focused() !== undefined && !covered();

  return (
    <AttachmentsContext.Provider value={{ library, open: setViewing }}>
      <HighlightContext.Provider value={highlighter}>
        <FilesContext.Provider value={files}>
          <AgentsContext.Provider value={{ has: agents.has, open: showAgent }}>
            <div class="screen session-screen">
              <header
                class="bar"
                hidden={covered() && Boolean(state())}
                data-agents={agents.strip().length > 0 || undefined}
              >
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
                      onChanges={openChanges}
                      onOpenFile={() => setChoosingFile(true)}
                      onScratchpad={openPad}
                    />
                  )}
                </Show>
                <Show when={agents.strip().length > 0}>
                  <AgentStrip agents={agents} mainNeedsYou={interaction() !== undefined} onShow={showAgent} />
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
                      {/* The transcript stays while Changes shows, so it keeps its place. */}
                      <Show when={changesOpen()}>
                        <ChangesView
                          api={ready.api}
                          session={current()}
                          fail={kit.fail}
                          refreshComments={refreshComments}
                          checkComments={checkComments}
                          onOpenFile={(path) => openFile({ path })}
                          onClose={closeChanges}
                        />
                      </Show>
                      <Show when={fileLocation()}>
                        {(location) => (
                          <FileView
                            api={ready.api}
                            session={current()}
                            location={location()}
                            fail={kit.fail}
                            refreshComments={refreshComments}
                            onClose={closeFile}
                          />
                        )}
                      </Show>
                      <Show when={padOpen()}>
                        <ScratchpadView handle={scratchpad} session={current()} onClose={closePad} />
                      </Show>
                      <div
                        class="transcript"
                        hidden={covered() || agents.focused() !== undefined}
                        ref={scroller}
                        onScroll={() => {
                          stick = atBottom();
                        }}
                      >
                        <Show when={current().hasMoreMessages}>
                          <button
                            type="button"
                            class="load-older"
                            disabled={loadingOlder()}
                            onClick={loadOlder}
                          >
                            {loadingOlder() ? "Loading…" : "Show earlier messages"}
                          </button>
                        </Show>
                        <Show when={rows().length > 0} fallback={<p class="empty">No messages yet.</p>}>
                          <For each={rows()}>
                            {(row, index) => <TranscriptRow row={row} last={index() === rows().length - 1} />}
                          </For>
                        </Show>
                      </div>
                      <For each={agents.visited().map((agent) => agent.id)}>
                        {(id) => (
                          <Show when={agents.visited().find((agent) => agent.id === id)}>
                            {(agent) => (
                              <AgentTranscript
                                agents={agents}
                                agent={agent()}
                                hidden={covered() || agents.focused()?.id !== id}
                              />
                            )}
                          </Show>
                        )}
                      </For>
                      <Show when={!agentInView() && outcome()}>
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
                      <OpenFileSheet
                        api={ready.api}
                        sessionId={props.id}
                        folder={displayPath(current().workspace?.cwd ?? current().info.cwd, home)}
                        open={choosingFile()}
                        fail={kit.fail}
                        onOpen={(path) => openFile({ path })}
                        onClose={() => setChoosingFile(false)}
                      />
                      <DeleteDialog
                        api={ready.api}
                        session={current()}
                        open={dialog() === "delete"}
                        onClose={() => setDialog(undefined)}
                      />
                      {/* The scratchpad has no composer; it's hidden, not removed, so
                  nothing being written or uploaded is lost. */}
                      <div class="dock-slot" hidden={padOpen() || agentInView()}>
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
                              onOpenChanges={openChanges}
                              onOpenFile={(path, line, end) => openFile({ path, line, end })}
                              checkComments={checkComments}
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
                      </div>
                      {/* A subagent takes no messages; its bar stands in for the composer. */}
                      <Show when={agentInView() && agents.focused()}>
                        {(agent) => <AgentDock agents={agents} agent={agent()} />}
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
          </AgentsContext.Provider>
        </FilesContext.Provider>
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
  onChanges(): void;
  onOpenFile(): void;
  onScratchpad(): void;
}

function SessionMenu(props: SessionMenuProps) {
  const kit = useKit();
  const ready = useReady();
  const [view, setView] = createSignal<"main" | "model" | "thinking">("main");
  const [models, setModels] = createSignal<ModelCapability[]>();
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [changes, setChanges] = createSignal<ChangeSummary | string>();
  let popover: HTMLDivElement | undefined;

  // The working tree's changes, read each time the menu opens.
  const loadChanges = async () => {
    const workspaceId = props.session.workspace?.workspaceId;
    if (!workspaceId) {
      setChanges("No folder");
      return;
    }
    try {
      const page = await ready.api.observeWorkingTree(props.session.info.id, { workspaceId, pageSize: 200 });
      setChanges(summarize(page.files, Boolean(page.nextCursor)));
    } catch (failure) {
      if (kit.fail(failure)) return;
      setChanges(
        failure instanceof ApiError && failure.code === "not_repository"
          ? "Not a Git repository"
          : (changesProblem(failure) ?? "Changes couldn’t be read"),
      );
    }
  };
  const comments = () => props.session.comments.length;

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
            void loadChanges();
          }
        }}
      >
        <Switch>
          <Match when={view() === "main"}>
            <button type="button" class="menu-action changes-row" onClick={() => act(props.onChanges)}>
              <Icon name="diff" size={18} />
              <span class="grow">
                <span>Changes</span>
                <span class="mono muted small">
                  <Switch fallback="Reading…">
                    <Match when={typeof changes() === "string" && (changes() as string)}>
                      {(problem) => <span class="changes-row-problem">{problem()}</span>}
                    </Match>
                    <Match when={typeof changes() === "object" && (changes() as ChangeSummary)}>
                      {(summary) => (
                        <Show when={summary().files > 0} fallback="No changes">
                          {filesLabel(summary())}
                          <Show when={summary().counts}>
                            {(counts) => (
                              <>
                                {" "}
                                <span class="added">+{counts().additions}</span>{" "}
                                <span class="removed">−{counts().deletions}</span>
                              </>
                            )}
                          </Show>
                        </Show>
                      )}
                    </Match>
                  </Switch>
                  <Show when={comments() > 0}> · {commentCount(comments())}</Show>
                </span>
              </span>
              <Icon name="chevRight" size={16} />
            </button>
            <button
              type="button"
              class="menu-action"
              disabled={!props.session.workspace}
              onClick={() => act(props.onOpenFile)}
            >
              <Icon name="file" size={18} />
              <span class="grow">Open File…</span>
            </button>
            <Show when={!props.session.info.temporary}>
              <button type="button" class="menu-action" onClick={() => act(props.onScratchpad)}>
                <Icon name="note" size={18} />
                <span class="grow">Scratchpad</span>
              </button>
            </Show>
            <hr />
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
              {(list) => (
                <ModelChoices
                  models={list()}
                  current={props.session.info.model}
                  disabled={(model) => busy() || !model.available || Boolean(props.session.activeTurnId)}
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
              current={props.session.info.thinkingLevel}
              disabled={busy()}
              onChoose={(level) => configure(props.session.info.model, level)}
            />
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
