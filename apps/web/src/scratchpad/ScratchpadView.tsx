// The scratchpad a session shares with its forks: a plain Markdown editor
// that saves a moment after typing stops. When the agent or another device
// changes the shared copy under unsaved edits, the draft stays and Review
// changes shows the two before either wins.
import {
  type Accessor,
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
import type { Api } from "../api/client";
import type { SessionInfo } from "../api/types";
import type { SessionState } from "../session/projection";
import { Icon, Spinner } from "../ui/icons";
import { compareCopies, localDrafts, Pad, type PadStatus } from "./pad";

export interface ScratchpadViewProps {
  handle: PadHandle;
  session: SessionState;
  onClose(): void;
}

/** Who the scratchpad is shared with, by the session that owns it. */
export function familyLabel(info: SessionInfo, owner: string | undefined): string {
  if (!owner || owner === info.id) return `Shared with forks of ${info.name || "this session"}`;
  if (owner === info.parentSessionId && info.parentSessionName)
    return `Shared with ${info.parentSessionName} and its forks`;
  return "Shared with the session this came from and its forks";
}

/** How long a save runs before the subtitle says so; a quick one would only flicker. */
export const SLOW_SAVE_MS = 400;

/**
 * The subtitle under the title: who it's shared with, a save that is taking
 * a while, or why it isn't saved.
 */
export function padSubtitle(
  status: PadStatus,
  loaded: boolean,
  family: string,
  slowSave = false,
): { text: string; tone?: "warn" | "danger" } {
  switch (status.kind) {
    case "loading":
      return { text: "Loading…" };
    case "saving":
      return { text: slowSave ? "Saving…" : family };
    case "conflict":
      return { text: "Not saved · The shared copy changed", tone: "warn" };
    case "failed":
      return loaded
        ? { text: `Not saved · ${status.reason}`, tone: "danger" }
        : { text: `Couldn’t load · ${status.reason}`, tone: "danger" };
    default:
      return { text: family };
  }
}

/** A session page's scratchpad: the pad and a signal that ticks when it changes. */
export interface PadHandle {
  pad: Pad;
  tick: Accessor<number>;
}

/**
 * The scratchpad for a session page. It lives as long as the page, so a draft
 * left in a conflict is still there when the scratchpad opens again. It reads
 * nothing until the scratchpad first opens; after that it saves unsaved edits
 * as the page goes away and reads the shared copy again when it comes back.
 * Call it while the page is being set up, so its listeners end with the page.
 */
export function createPadHandle(api: Api, id: string, fail: (error: unknown) => boolean): PadHandle {
  const [tick, setTick] = createSignal(0);
  const call = async <T,>(run: () => Promise<T>): Promise<T> => {
    try {
      return await run();
    } catch (error) {
      fail(error);
      throw error;
    }
  };
  const pad = new Pad(
    {
      get: () => call(() => api.getScratchpad(id)),
      put: (content, expectedRevision, keepalive) =>
        call(() => api.updateScratchpad(id, { content, expectedRevision }, { keepalive })),
    },
    (run, ms) => {
      const timer = setTimeout(run, ms);
      return () => clearTimeout(timer);
    },
    () => setTick((value) => value + 1),
    localDrafts(),
  );
  const visibility = () => {
    if (document.visibilityState === "hidden") pad.flush();
    else if (pad.shared)
      void api.getScratchpad(id).then(
        (record) => pad.observe(record),
        () => {},
      );
  };
  const leave = () => pad.flush();
  document.addEventListener("visibilitychange", visibility);
  window.addEventListener("pagehide", leave);
  onCleanup(() => {
    document.removeEventListener("visibilitychange", visibility);
    window.removeEventListener("pagehide", leave);
    pad.flush();
    pad.dispose();
  });
  return { pad, tick };
}

export function ScratchpadView(props: ScratchpadViewProps) {
  const pad = props.handle.pad;
  // Reads of the pad go through `view` so they follow its changes.
  const view =
    <T,>(read: (value: Pad) => T) =>
    () => {
      props.handle.tick();
      return read(pad);
    };
  const status = view((value) => value.status);
  const reviewing = view((value) => value.reviewing);
  const loaded = view((value) => Boolean(value.shared));
  const version = view((value) => value.version);
  const saving = createMemo(() => status().kind === "saving");
  const [slowSave, setSlowSave] = createSignal(false);
  createEffect(() => {
    if (!saving()) {
      setSlowSave(false);
      return;
    }
    const timer = setTimeout(() => setSlowSave(true), SLOW_SAVE_MS);
    onCleanup(() => clearTimeout(timer));
  });
  const subtitle = createMemo(() =>
    padSubtitle(
      status(),
      loaded(),
      familyLabel(props.session.info, pad.shared?.ownerSessionId ?? props.session.scratchpad?.ownerSessionId),
      slowSave(),
    ),
  );
  const review = createMemo(() => {
    props.handle.tick();
    return reviewing() && pad.reviewed ? compareCopies(pad.reviewed.content, pad.draft) : [];
  });
  const [busy, setBusy] = createSignal(false);
  let field: HTMLTextAreaElement | undefined;

  onMount(() => {
    if (!pad.shared) void pad.load();
  });
  // A typed draft keeps its caret; a replaced one loads into the field.
  createEffect(
    on(version, () => {
      if (field && field.value !== pad.draft) field.value = pad.draft;
    }),
  );
  // Leaving the screen saves what's typed.
  onCleanup(() => pad.flush());

  const back = () => {
    if (reviewing()) pad.keepEditing();
    else props.onClose();
  };
  const replace = async () => {
    setBusy(true);
    try {
      await pad.replaceShared();
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <header class="bar">
        <button type="button" class="icon-button" aria-label="Back" onClick={back}>
          <Icon name="back" />
        </button>
        <div class="bar-title">
          <h1>{reviewing() ? "Scratchpad changes" : "Scratchpad"}</h1>
          <Show when={!reviewing()} fallback={<p class="muted">− shared copy · + your draft</p>}>
            <p class="pad-subtitle" data-tone={subtitle().tone} role="status">
              {subtitle().text}
            </p>
          </Show>
        </div>
        <Show when={status().kind === "failed" && loaded() && !reviewing()}>
          <button type="button" data-size="small" class="pad-retry" onClick={() => void pad.save()}>
            Retry
          </button>
        </Show>
      </header>
      <Switch>
        <Match when={!loaded() && status().kind === "failed"}>
          <div class="empty-state">
            <p>The scratchpad couldn’t be read.</p>
            <button type="button" onClick={() => void pad.load()}>
              Try again
            </button>
          </div>
        </Match>
        <Match when={!loaded()}>
          <p class="empty">
            <Spinner /> Reading the scratchpad…
          </p>
        </Match>
        <Match when={reviewing()}>
          <section class="pad-review" aria-label="Scratchpad changes">
            <For each={review()}>
              {(line) => (
                <div class="pad-line" data-kind={line.kind}>
                  <span class="pad-sign" aria-hidden="true">
                    {line.kind === "shared" ? "−" : line.kind === "draft" ? "+" : ""}
                  </span>
                  <span class="pad-text">
                    <Show when={line.kind !== "same"}>
                      <span class="visually-hidden">{line.kind === "shared" ? "Shared: " : "Yours: "}</span>
                    </Show>
                    {line.text || "\u200b"}
                  </span>
                </div>
              )}
            </For>
          </section>
          <div class="pad-actions">
            <button type="button" data-variant="primary" onClick={() => pad.keepEditing()}>
              Keep editing
            </button>
            <div class="pad-actions-row">
              <button type="button" disabled={busy()} onClick={() => pad.useShared()}>
                Use shared
              </button>
              <button type="button" class="pad-replace" disabled={busy()} onClick={replace}>
                Replace shared with mine
              </button>
            </div>
          </div>
        </Match>
        <Match when={loaded()}>
          <div class="pad">
            <Show when={status().kind === "conflict"}>
              <div class="pad-conflict" role="alert">
                <p>
                  <Icon name="alert" size={16} />
                  <span>The shared scratchpad changed while you were editing. Your draft is kept.</span>
                </p>
                <button type="button" data-size="small" onClick={() => pad.review()}>
                  Review changes
                </button>
              </div>
            </Show>
            <textarea
              ref={(element) => {
                field = element;
                element.value = pad.draft;
              }}
              class="pad-field"
              aria-label="Scratchpad"
              placeholder="Notes you and the agent share across this session and its forks."
              spellcheck={false}
              onInput={(event) => pad.edit(event.currentTarget.value)}
            />
          </div>
        </Match>
      </Switch>
    </>
  );
}
