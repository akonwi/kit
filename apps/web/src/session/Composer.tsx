// The composer: a field that sends, queues behind a run, or stops it, with
// the run's activity line, the queued follow-ups, and the message's
// attachments above it.
import { createMemo, createSignal, For, Match, onCleanup, onMount, Show, Switch, useContext } from "solid-js";
import type { Api } from "../api/client";
import { bytes } from "../ui/format";
import { Icon, Spinner } from "../ui/icons";
import { AttachmentsContext, FileTile, Thumbnail } from "./AttachmentViews";
import type { Draft, DraftAttachment } from "./attachments";
import type { SessionState, ToolCall } from "./projection";
import { type Shell, shellDraft, toggleShellContext } from "./shell";
import { useClock } from "./Transcript";
import { formatDuration, presentTool } from "./tools";
import type { Connection } from "./watch";

export interface ComposerProps {
  api: Api;
  session: SessionState;
  connection: Connection;
  /** Reports a failure the app may handle; returns true when it did. */
  fail(error: unknown): boolean;
  reconnect(): void;
  /** The message being written, which outlives the composer. */
  draft: Draft;
  /** Why the draft's text didn't send, such as a fork's first message. */
  initialError?: string;
  /** The session's shell commands, which a draft starting with `!` runs. */
  shell: Shell;
  /** The row a command sent now follows: the last one shown, or "" for none. */
  anchor(): string;
}

/** The last call still running, which the activity line names. */
function runningTool(session: SessionState): ToolCall | undefined {
  for (let index = session.rows.length - 1; index >= 0; index -= 1) {
    const row = session.rows[index];
    if (row?.kind !== "tools") continue;
    const tool = row.tools.findLast((value) => value.status === "running");
    if (tool) return tool;
  }
  return undefined;
}

export function Composer(props: ComposerProps) {
  const draft = () => props.draft;
  const text = () => draft().text();
  const setText = (value: string) => draft().setText(value);
  const [sending, setSending] = createSignal(false);
  const [error, setError] = createSignal(props.initialError ?? "");
  const [trayBusy, setTrayBusy] = createSignal(false);
  const [dropping, setDropping] = createSignal(false);
  let field: HTMLTextAreaElement | undefined;
  let picker: HTMLInputElement | undefined;

  const live = () => props.connection === "live";
  const running = () => Boolean(props.session.activeTurnId);
  const items = () => draft().items();
  const empty = () => text().trim() === "" && items().length === 0;
  const uploading = () => !draft().ready();
  const notice = () =>
    draft().notice() ??
    (draft().blocked() ? { tone: "warning" as const, text: draft().blocked() ?? "" } : undefined);
  const queued = () => props.session.followUps.count;
  // A draft starting with ! is a shell command, !! one the agent doesn't see.
  const shellMode = createMemo(() => shellDraft(text()));
  const shellActive = () => props.shell.active();

  const fail = (failure: unknown, fallback: string) => {
    if (props.fail(failure)) return;
    setError(failure instanceof Error ? failure.message : fallback);
  };

  const runShell = async () => {
    if (!shellMode()?.command || sending() || !live() || shellActive()) return;
    setSending(true);
    setError("");
    try {
      if (await props.shell.start(text(), props.anchor())) setText("");
    } catch (failure) {
      if (!props.fail(failure)) {
        const reason = failure instanceof Error ? failure.message : "";
        setError(reason ? `Didn’t run. ${reason}` : "Didn’t run.");
      }
    } finally {
      setSending(false);
      field?.focus();
    }
  };

  const stopShell = async () => {
    setError("");
    try {
      await props.shell.stop();
    } catch (failure) {
      fail(failure, "Couldn’t stop the command.");
    }
  };

  const send = async () => {
    if (shellMode()) return runShell();
    const value = text().trim();
    if (empty() || uploading() || draft().blocked() || sending() || !live()) return;
    const attachmentIds = draft().ids();
    setSending(true);
    setError("");
    try {
      await props.api.submitPrompt(props.session.info.id, {
        text: value,
        ...(attachmentIds.length > 0 ? { attachmentIds } : {}),
      });
      draft().acknowledge(attachmentIds);
    } catch (failure) {
      if (!props.fail(failure)) {
        const reason = failure instanceof Error ? failure.message : "";
        setError(reason ? `Didn’t send. ${reason}` : "Didn’t send.");
      }
    } finally {
      setSending(false);
      field?.focus();
    }
  };

  const stop = async () => {
    const turn = props.session.activeTurnId;
    if (!turn || !live()) return;
    setError("");
    try {
      await props.api.abortTurn(props.session.info.id, turn);
    } catch (failure) {
      fail(failure, "Couldn’t stop the run.");
    }
  };

  const editQueued = async () => {
    setTrayBusy(true);
    try {
      const result = await props.api.restoreFollowUps(props.session.info.id);
      const restored = result.messages.map((message) => message.text).filter(Boolean);
      setText([text().trim(), ...restored].filter(Boolean).join("\n\n"));
      field?.focus();
      // Send waits for these, as it does for uploads.
      await draft().restore(result.messages.flatMap((message) => message.attachmentIds ?? []));
    } catch (failure) {
      fail(failure, "Couldn’t edit the queued messages.");
    } finally {
      setTrayBusy(false);
    }
  };

  const clearQueued = async () => {
    setTrayBusy(true);
    try {
      await props.api.restoreFollowUps(props.session.info.id);
    } catch (failure) {
      fail(failure, "Couldn’t clear the queued messages.");
    } finally {
      setTrayBusy(false);
    }
  };

  const sendQueuedNow = async () => {
    setTrayBusy(true);
    try {
      await props.api.promoteFollowUps(props.session.info.id);
    } catch (failure) {
      fail(failure, "Couldn’t send the queued messages.");
    } finally {
      setTrayBusy(false);
    }
  };

  // Files dragged over the composer can land in it; anywhere else on the
  // page a drop does nothing, rather than the browser opening the file.
  const carriesFiles = (event: DragEvent) => event.dataTransfer?.types.includes("Files") ?? false;
  let depth = 0;
  const ignoreDrop = (event: DragEvent) => {
    if (carriesFiles(event)) event.preventDefault();
  };
  onMount(() => {
    window.addEventListener("dragover", ignoreDrop);
    window.addEventListener("drop", ignoreDrop);
  });
  onCleanup(() => {
    window.removeEventListener("dragover", ignoreDrop);
    window.removeEventListener("drop", ignoreDrop);
  });
  const dragEnter = (event: DragEvent) => {
    if (!carriesFiles(event)) return;
    event.preventDefault();
    depth += 1;
    setDropping(true);
  };
  const dragLeave = (event: DragEvent) => {
    if (!carriesFiles(event)) return;
    depth = Math.max(0, depth - 1);
    if (depth === 0) setDropping(false);
  };
  const drop = (event: DragEvent) => {
    if (!carriesFiles(event)) return;
    event.preventDefault();
    depth = 0;
    setDropping(false);
    draft().add([...(event.dataTransfer?.files ?? [])]);
  };

  // A pasted image attaches; pasted text stays text.
  const paste = (event: ClipboardEvent) => {
    const data = event.clipboardData;
    if (!data || data.types.includes("text/plain")) return;
    const files = [...data.files];
    if (files.length === 0) return;
    event.preventDefault();
    draft().add(files);
  };

  const keydown = (event: KeyboardEvent) => {
    // Enter adds a line; a modifier sends, as on a keyboard-first desktop.
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      void send();
    }
  };

  return (
    <section
      class="composer"
      aria-label="Composer"
      onDragEnter={dragEnter}
      onDragOver={(event) => {
        if (carriesFiles(event)) event.preventDefault();
      }}
      onDragLeave={dragLeave}
      onDrop={drop}
    >
      <Switch>
        <Match when={error()}>
          <div class="activity error" role="alert">
            <Icon name="alert" size={14} />
            <span>{error()}</span>
          </div>
        </Match>
        <Match when={props.connection !== "live"}>
          <div class="activity">
            <Spinner />
            <span class="activity-label grow">
              {props.connection === "connecting" ? "Connecting…" : "Reconnecting…"}
            </span>
            <Show when={props.connection === "reconnecting"}>
              <button type="button" class="link" onClick={() => props.reconnect()}>
                Retry now
              </button>
            </Show>
          </div>
        </Match>
        <Match when={running() || props.session.compacting}>
          <Activity session={props.session} />
        </Match>
      </Switch>
      <Show when={queued() > 0}>
        <div class="queued">
          <div class="queued-head">
            <span class="muted grow">{queued()} queued</span>
            <button type="button" class="link" disabled={trayBusy()} onClick={editQueued}>
              Edit
            </button>
            <button type="button" class="link" disabled={trayBusy()} onClick={clearQueued}>
              Clear
            </button>
            <button
              type="button"
              data-variant="primary"
              data-size="small"
              disabled={trayBusy()}
              onClick={sendQueuedNow}
            >
              Send now
            </button>
          </div>
          <Show when={(props.session.followUps.previews ?? []).length > 0}>
            <ol class="queued-list">
              <For each={props.session.followUps.previews}>{(preview) => <li>{preview}</li>}</For>
            </ol>
          </Show>
        </div>
      </Show>
      <Show when={notice()}>
        {(value) => (
          <div class="draft-notice" data-tone={value().tone} role="status">
            <Icon name={value().tone === "warning" ? "alert" : "alertCircle"} size={16} />
            <span class="grow">{value().text}</span>
            <Show when={draft().notice()}>
              <button
                type="button"
                class="icon-button small"
                aria-label="Dismiss"
                onClick={() => draft().dismissNotice()}
              >
                <Icon name="close" size={16} />
              </button>
            </Show>
          </div>
        )}
      </Show>
      <Show
        when={dropping()}
        fallback={
          <Show when={items().length > 0}>
            <ul class="draft-strip" aria-label="Attachments">
              <For each={items()}>{(item) => <DraftTile item={item} draft={draft()} />}</For>
            </ul>
          </Show>
        }
      >
        <div class="drop-zone">
          <Icon name="plus" size={18} />
          <span>Drop to attach</span>
        </div>
      </Show>
      <Show when={shellMode()}>
        {(mode) => (
          <div class="shell-mode">
            <Icon name="terminal" size={12} />
            <span>Shell</span>
            {/* As on the Mac: the label says what the command does now, and a tap switches ! and !!. */}
            <button
              type="button"
              class="shell-context"
              aria-pressed={!mode().excluded}
              onClick={() => {
                setText(toggleShellContext(text()));
                field?.focus();
              }}
            >
              {mode().excluded ? "Excluded from context" : "Include in context"}
            </button>
          </div>
        )}
      </Show>
      <form
        class="composer-field"
        data-dropping={dropping() || undefined}
        onSubmit={(event) => {
          event.preventDefault();
          void send();
        }}
      >
        <Show when={!shellMode()}>
          <button
            type="button"
            class="icon-button attach"
            aria-label="Attach files"
            onClick={() => picker?.click()}
          >
            <Icon name="plus" />
          </button>
        </Show>
        <input
          ref={picker}
          type="file"
          multiple
          hidden
          onChange={(event) => {
            draft().add([...(event.currentTarget.files ?? [])]);
            event.currentTarget.value = "";
          }}
        />
        <label for="composer-text" class="visually-hidden">
          Message
        </label>
        <textarea
          id="composer-text"
          ref={field}
          rows={1}
          placeholder={running() ? "Follow up…" : "Message"}
          value={text()}
          readOnly={sending()}
          data-sending={sending() || undefined}
          data-shell={shellMode() ? "" : undefined}
          onInput={(event) => {
            setText(event.currentTarget.value);
            if (error()) setError("");
          }}
          onKeyDown={keydown}
          onPaste={paste}
          enterkeyhint="enter"
        />
        <Switch>
          <Match when={sending()}>
            <button type="button" class="send" data-variant="primary" disabled aria-label="Sending">
              <Spinner size={16} class="on-primary" />
            </button>
          </Match>
          <Match when={shellActive()}>
            <button
              type="button"
              class="send"
              data-variant="primary"
              disabled={!live() || props.shell.stopping()}
              onClick={stopShell}
              aria-label="Stop shell command"
            >
              <span class="stop-mark" />
            </button>
          </Match>
          <Match when={running() && empty()}>
            <button
              type="button"
              class="send"
              data-variant="primary"
              disabled={!live()}
              onClick={stop}
              aria-label="Stop"
            >
              <span class="stop-mark" />
            </button>
          </Match>
          <Match when={true}>
            <button
              type="submit"
              class="send"
              data-variant="primary"
              disabled={
                shellMode()
                  ? !shellMode()?.command || !live()
                  : empty() || uploading() || Boolean(draft().blocked()) || !live()
              }
              aria-label={shellMode() ? "Run" : uploading() ? "Send, after the uploads finish" : "Send"}
            >
              <Icon name="up" size={18} />
            </button>
          </Match>
        </Switch>
      </form>
    </section>
  );
}

/** One file in the strip: a thumbnail or a tile, with ×, and Retry if it didn't upload. */
function DraftTile(props: { item: DraftAttachment; draft: Draft }) {
  const context = useContext(AttachmentsContext);
  const item = () => props.item;
  const url = () =>
    item().preview ?? (item().info ? context?.library.image(item().info?.id ?? "") : undefined);
  const remove = () => props.draft.remove(item().key);
  const retry = () => props.draft.retry(item().key);
  const Close = () => (
    <button type="button" class="tile-remove" aria-label={`Remove ${item().filename}`} onClick={remove}>
      <Icon name="close" size={12} />
    </button>
  );
  return (
    <li class="draft-tile" data-busy={item().busy || undefined} data-failed={item().error ? "" : undefined}>
      <Show
        when={item().image}
        fallback={
          <div class="file-tile" title={item().error}>
            <FileTile
              name={item().filename}
              detail={
                <Switch fallback={<span class="mono">{bytes(item().size)}</span>}>
                  <Match when={item().error}>
                    <span class="danger">Didn’t upload</span>
                    <button type="button" class="link" onClick={retry}>
                      Retry
                    </button>
                  </Match>
                  <Match when={item().busy}>
                    <span>Uploading…</span>
                  </Match>
                </Switch>
              }
            />
            <Close />
          </div>
        }
      >
        <Thumbnail url={url()} label={item().filename}>
          <Show when={item().busy}>
            <span class="thumb-overlay">
              <Spinner size={18} />
            </span>
          </Show>
          <Show when={item().error}>
            <button
              type="button"
              class="thumb-overlay retry"
              title={item().error}
              aria-label={`Retry ${item().filename}, which didn’t upload`}
              onClick={retry}
            >
              <Icon name="retry" size={18} />
            </button>
          </Show>
          <Close />
        </Thumbnail>
      </Show>
    </li>
  );
}

function Activity(props: { session: SessionState }) {
  const tool = createMemo(() => runningTool(props.session));
  const now = useClock();
  const retryIn = () => {
    const at = Date.parse(props.session.retryAt ?? "");
    return Number.isNaN(at) ? 0 : Math.max(0, Math.ceil((at - now()) / 1000));
  };
  return (
    <Show
      when={props.session.retryAt}
      fallback={<Working session={props.session} tool={tool()} now={now()} />}
    >
      <div class="activity retrying">
        <Icon name="retry" size={14} />
        <span class="activity-label">Retrying in {retryIn()}s</span>
        <Show when={props.session.retryCount}>
          {(count) => <span class="retry-attempt mono grow">attempt {count()}</span>}
        </Show>
      </div>
    </Show>
  );
}

function Working(props: { session: SessionState; tool: ToolCall | undefined; now: number }) {
  const tool = () => props.tool;
  const now = () => props.now;
  return (
    <div class="activity">
      <Spinner />
      <Show
        when={tool()}
        fallback={<span class="activity-label grow">{props.session.activity ?? "Working…"}</span>}
      >
        {(current) => {
          const presentation = () => presentTool(current());
          return (
            <>
              <span class="activity-label">{presentation().title}</span>
              <span class="activity-detail mono grow">{presentation().chip ?? ""}</span>
              <Show when={current().startedAt}>
                {(started) => <span class="mono muted small">{formatDuration(now() - started())}</span>}
              </Show>
            </>
          );
        }}
      </Show>
    </div>
  );
}
