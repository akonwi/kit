// The session list: every session on the server in the server's attention
// order (waiting for you, then working, then the rest), or newest first from a
// server that doesn't order it, with the ones waiting for an answer marked and
// filterable.
import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { ApiError, NetworkError } from "../api/client";
import type { SessionInfo } from "../api/types";
import { useKit, useReady } from "../app/kit";
import { navigate } from "../app/router";
import { NewSessionSheet } from "../session/NewSession";
import { displayPath, relativeTime, serverUserHome, shortDate } from "../ui/format";
import { Icon, Logo, NeedsBadge, Spinner } from "../ui/icons";

type Activity = "running" | "awaitingResponse";

const POLL_MS = 4000;

export function SessionsPage() {
  const kit = useKit();
  const ready = useReady();
  const [sessions, setSessions] = createSignal<SessionInfo[] | undefined>();
  // Every session, temporary ones too, for the new session sheet's recent folders.
  const [everySession, setEverySession] = createSignal<SessionInfo[]>([]);
  const [loadedAt, setLoadedAt] = createSignal<Date>();
  const [unreachable, setUnreachable] = createSignal(false);
  const [creating, setCreating] = createSignal(false);
  const [activity, setActivity] = createSignal<Record<string, Activity>>({});
  const [filter, setFilter] = createSignal<"all" | "needs">("all");
  const [error, setError] = createSignal("");
  const [now, setNow] = createSignal(new Date());
  const home = serverUserHome(ready.health.home);

  let timer: ReturnType<typeof setTimeout> | undefined;
  let controller: AbortController | undefined;
  // Whether the server orders the list by attention (ADR 0061). An older
  // server ignores the request or refuses it; the page then asks for activity
  // separately and keeps its own order for the rest of its life.
  let attention = true;

  const load = async (signal: AbortSignal) => {
    if (attention) {
      try {
        const list = await ready.api.listSessions(signal, "attention");
        if (list.order === "attention") {
          const statuses: Record<string, Activity> = {};
          for (const session of list.sessions) {
            if (session.activity) statuses[session.id] = session.activity.status;
          }
          return { sessions: list.sessions, statuses, ordered: true };
        }
      } catch (failure) {
        if (!(failure instanceof ApiError && failure.status === 400)) throw failure;
      }
      attention = false;
    }
    const [list, active] = await Promise.all([
      ready.api.listSessions(signal),
      ready.api.sessionActivity(signal),
    ]);
    const statuses: Record<string, Activity> = {};
    for (const entry of active.sessions) {
      if (entry.status === "running" || entry.status === "awaitingResponse")
        statuses[entry.sessionId] = entry.status;
    }
    return { sessions: list.sessions, statuses, ordered: false };
  };

  const refresh = async () => {
    clearTimeout(timer);
    controller?.abort();
    controller = new AbortController();
    try {
      const list = await load(controller.signal);
      const statuses = list.statuses;
      setEverySession(list.sessions);
      const listed = list.sessions.filter((session) => !session.temporary);
      if (!list.ordered) listed.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
      setSessions(listed);
      setActivity(statuses);
      setNow(new Date());
      setLoadedAt(new Date());
      setError("");
      setUnreachable(false);
    } catch (failure) {
      if (failure instanceof DOMException && failure.name === "AbortError") return;
      if (kit.fail(failure)) return;
      setNow(new Date());
      if (failure instanceof NetworkError) {
        setUnreachable(true);
        setError("");
      } else {
        setUnreachable(false);
        setError(failure instanceof Error ? failure.message : "Sessions couldn’t be loaded.");
      }
    }
    if (document.visibilityState === "visible") timer = setTimeout(refresh, POLL_MS);
  };

  const visibility = () => {
    if (document.visibilityState === "visible") void refresh();
    else clearTimeout(timer);
  };

  onMount(() => {
    void refresh();
    document.addEventListener("visibilitychange", visibility);
  });
  onCleanup(() => {
    clearTimeout(timer);
    controller?.abort();
    document.removeEventListener("visibilitychange", visibility);
  });

  const needs = createMemo(() =>
    (sessions() ?? []).filter((session) => activity()[session.id] === "awaitingResponse"),
  );
  const shown = () => (filter() === "needs" ? needs() : (sessions() ?? []));

  return (
    <div class="screen">
      <header class="bar">
        <Logo height={18} />
        <div class="bar-title">
          <h1>Sessions</h1>
          <p class="mono muted">{kit.host}</p>
        </div>
        <button
          type="button"
          class="icon-button"
          aria-label="New session"
          disabled={unreachable()}
          onClick={() => setCreating(true)}
        >
          <Icon name="plus" />
        </button>
        <BrowserMenu />
      </header>
      <Show when={unreachable()}>
        <div class="status-banner" role="alert">
          <Icon name="alertCircle" size={18} class="warn" />
          <span class="grow">
            <strong>Can’t reach {kit.host}</strong>
            <span class="muted small">
              {loadedAt()
                ? `Showing the list from ${ago(loadedAt() as Date, now())}.`
                : "Kit’s server can’t be reached."}
            </span>
          </span>
          <button type="button" onClick={() => void refresh()}>
            Retry
          </button>
        </div>
      </Show>
      <Show when={sessions()?.length !== 0}>
        <div class="filter">
          <fieldset class="segmented">
            <legend class="visually-hidden">Show</legend>
            <label>
              <input
                type="radio"
                name="filter"
                value="all"
                checked={filter() === "all"}
                onChange={() => setFilter("all")}
              />
              All
            </label>
            <label>
              <input
                type="radio"
                name="filter"
                value="needs"
                checked={filter() === "needs"}
                onChange={() => setFilter("needs")}
              />
              Needs you
              <Show when={needs().length > 0}>
                <span class="count">{needs().length}</span>
              </Show>
            </label>
          </fieldset>
        </div>
      </Show>
      <Show when={error()}>
        <p class="banner error" role="alert">
          {error()}
        </p>
      </Show>
      <Show when={sessions()} fallback={<p class="empty">Loading sessions…</p>}>
        <Show
          when={shown().length > 0}
          fallback={
            <Show when={filter() === "all"} fallback={<p class="empty">Nothing is waiting for you.</p>}>
              <div class="empty-state">
                <Icon name="folder" size={32} class="faint" />
                <span class="empty-copy">
                  <strong>No sessions yet</strong>
                  <span>Sessions on {kit.host} appear here, wherever they started.</span>
                </span>
                <button
                  type="button"
                  data-variant="primary"
                  disabled={unreachable()}
                  onClick={() => setCreating(true)}
                >
                  <Icon name="plus" size={16} />
                  New session
                </button>
              </div>
            </Show>
          }
        >
          <ul class="session-list" data-stale={unreachable() || undefined}>
            <For each={shown()}>
              {(session) => (
                <SessionRow session={session} activity={activity()[session.id]} home={home} now={now()} />
              )}
            </For>
          </ul>
        </Show>
      </Show>
      <NewSessionSheet open={creating()} onClose={() => setCreating(false)} sessions={everySession()} />
    </div>
  );
}

/** How long ago the list loaded: a moment ago, 2m ago, 1h ago. */
function ago(then: Date, now: Date): string {
  const minutes = Math.floor((now.getTime() - then.getTime()) / 60_000);
  if (minutes < 1) return "a moment ago";
  if (minutes < 60) return `${minutes}m ago`;
  return `${Math.floor(minutes / 60)}h ago`;
}

function SessionRow(props: { session: SessionInfo; activity?: Activity; home?: string; now: Date }) {
  const href = () => `/sessions/${encodeURIComponent(props.session.id)}`;
  const open = (event: MouseEvent) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    navigate(href());
  };
  return (
    <li class="session-row" data-activity={props.activity ?? "idle"}>
      <a href={href()} onClick={open}>
        <span class="mark">
          <Show when={props.activity === "awaitingResponse"}>
            <NeedsBadge />
          </Show>
          <Show when={props.activity === "running"}>
            <Spinner size={16} />
          </Show>
        </span>
        <span class="body">
          <span class="line">
            <span class="title">{props.session.name || "Untitled session"}</span>
            <span class="when">{relativeTime(props.session.updatedAt, props.now)}</span>
          </span>
          <Show when={props.activity}>
            <span class="state">{props.activity === "awaitingResponse" ? "Waiting for you" : "Working"}</span>
          </Show>
          <span class="path mono">{displayPath(props.session.cwd, props.home)}</span>
        </span>
      </a>
    </li>
  );
}

function BrowserMenu() {
  const kit = useKit();
  const ready = useReady();
  const [error, setError] = createSignal("");
  const signOut = async () => {
    setError("");
    try {
      await kit.signOut();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Signing out failed.");
    }
  };
  return (
    <>
      <button type="button" class="icon-button" popovertarget="browser-menu" aria-label="Menu">
        <Icon name="more" />
      </button>
      <div id="browser-menu" popover class="menu sheet-menu">
        <div class="menu-section">
          <span class="eyebrow">This browser</span>
          <span>{ready.credential.deviceName}</span>
          <span class="muted small">Paired {shortDate(ready.credential.pairedAt)}</span>
        </div>
        <hr />
        <button type="button" data-variant="danger" onClick={signOut}>
          <Icon name="signout" size={18} />
          Sign out
        </button>
        <Show when={error()}>
          <p class="error small">{error()}</p>
        </Show>
      </div>
    </>
  );
}
