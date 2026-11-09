// The Changes screen: a session folder's changes against a target the reader
// picks (uncommitted changes, this branch, or a commit), file by file, with
// the comments that go with the next message.
import { createEffect, createMemo, createSignal, For, Match, on, onCleanup, Show, Switch } from "solid-js";
import { type Api, ApiError, isAbort } from "../api/client";
import type { DiffFileSummary, DiffTargetCatalog, DiffTargetEntry, WorkingTreePage } from "../api/types";
import type { SessionState } from "../session/projection";
import { relativeTime } from "../ui/format";
import { Icon, Spinner } from "../ui/icons";
import type { Comment } from "./comments";
import { filesLabel, readable, type Selection, summarize } from "./diff";
import { describe, FileDiff, movedOn, Note, type Review } from "./FileDiff";

/** How many files one observation page holds. */
const FILE_PAGE = 200;

interface Observed {
  targetId: string;
  revision: string;
  files: DiffFileSummary[];
  nextCursor?: string;
}

export interface ChangesProps {
  api: Api;
  session: SessionState;
  fail(error: unknown): boolean;
  refreshComments(): Promise<void>;
  /** Reads uncommitted changes again, then the comments. */
  checkComments(): Promise<void>;
  /** Opens a file as it is now. */
  onOpenFile(path: string): void;
  onClose(): void;
}

/** The changes a failed read leaves the reader with, in words. */
export function changesProblem(error: unknown): string | undefined {
  if (!(error instanceof ApiError)) return undefined;
  switch (error.code) {
    case "not_repository":
      return "This session’s folder isn’t in a Git repository.";
    case "unsupported_repository":
      return "Kit can’t read changes in this repository.";
    case "repository_unavailable":
      return "The repository can’t be read right now.";
    case "capacity_exceeded":
    case "rate_limited":
      return "The server is busy reading changes. Try again in a moment.";
    default:
      return undefined;
  }
}

/** The short name the picker's button shows for a target. */
export function targetButtonLabel(entry: DiffTargetEntry | undefined): string {
  if (!entry || entry.kind === "working_tree") return "Uncommitted";
  if (entry.kind === "branch") return "This branch";
  return shortOid(entry.metadata.abbreviatedOid) || "Commit";
}

/** A commit id at the length people read: 7 characters. */
export function shortOid(oid: string | undefined): string {
  return (oid ?? "").slice(0, 7);
}

export function commentCount(count: number): string {
  return count === 1 ? "1 comment" : `${count} comments`;
}

export function ChangesView(props: ChangesProps) {
  const id = () => props.session.info.id;
  const workspaceId = () => props.session.workspace?.workspaceId;
  const [choice, setChoice] = createSignal<DiffTargetEntry>();
  const [catalog, setCatalog] = createSignal<DiffTargetCatalog>();
  const [catalogError, setCatalogError] = createSignal("");
  const [observed, setObserved] = createSignal<Observed>();
  const [problem, setProblem] = createSignal("");
  const [loading, setLoading] = createSignal(false);
  const [loadingMore, setLoadingMore] = createSignal(false);
  const [open, setOpen] = createSignal(new Set<string>());
  const [selection, setSelection] = createSignal<Selection>();
  const [replacing, setReplacing] = createSignal<Comment>();
  const [editing, setEditing] = createSignal<number>();
  const [generation, setGeneration] = createSignal(0);
  const [moveFailed, setMoveFailed] = createSignal<number>();
  // A target whose reference expired is listed again and observed once more.
  let retrying = false;
  let controller: AbortController | undefined;
  onCleanup(() => controller?.abort());

  const fail = (error: unknown) => props.fail(error);
  const refreshComments = () => props.refreshComments();

  const loadCatalog = async (): Promise<DiffTargetCatalog | undefined> => {
    const workspace = workspaceId();
    if (!workspace) return undefined;
    try {
      const value = await props.api.listDiffTargets(id(), workspace);
      setCatalog(value);
      setCatalogError("");
      return value;
    } catch (failure) {
      if (fail(failure)) return undefined;
      setCatalogError(
        changesProblem(failure) ?? describe({ fail }, failure, "Targets couldn’t be listed.") ?? "",
      );
      return undefined;
    }
  };

  const page = (
    target: DiffTargetEntry | undefined,
    signal: AbortSignal,
    cursor?: string,
    revision?: string,
  ) => {
    const workspace = workspaceId() ?? "";
    if (!target || target.kind === "working_tree") {
      return props.api.observeWorkingTree(
        id(),
        { workspaceId: workspace, pageSize: FILE_PAGE, ...(cursor ? { cursor } : {}) },
        signal,
      );
    }
    return props.api.observeDiff(
      id(),
      {
        workspaceId: workspace,
        targetReference: target.reference,
        expectedTargetId: target.targetId,
        pageSize: FILE_PAGE,
        ...(cursor && revision ? { cursor, expectedTargetRevision: revision } : {}),
      },
      signal,
    );
  };

  const take = (result: WorkingTreePage) => {
    const previous = observed();
    const files = result.files;
    setObserved({
      targetId: result.observation.target.id,
      revision: result.observation.revision,
      files,
      nextCursor: result.nextCursor,
    });
    setGeneration(generation() + 1);
    // The first file opens; files the reader opened stay open.
    if (!previous || previous.targetId !== result.observation.target.id) {
      const firstReadable = files.find(readable);
      setOpen(new Set(firstReadable ? [firstReadable.path] : []));
    }
  };

  /** Reads the target's changed files again; a target whose reference expired is listed again once. */
  const observe = async (retried = false) => {
    if (!workspaceId()) {
      setProblem("This session has no folder to compare.");
      return;
    }
    controller?.abort();
    const current = new AbortController();
    controller = current;
    setLoading(true);
    setProblem("");
    const target = choice();
    try {
      const result = await page(target, current.signal);
      if (controller !== current) return;
      take(result);
      // Reading uncommitted changes renewed the server's hold on them; other
      // targets leave comments on uncommitted changes to be checked.
      const check = !target || target.kind === "working_tree" ? refreshComments : props.checkComments;
      void check().catch(() => {});
    } catch (failure) {
      if (isAbort(failure) || controller !== current) return;
      if (fail(failure)) return;
      if (!retried && target && target.kind !== "working_tree" && movedOn(failure)) {
        const fresh = await loadCatalog();
        if (controller !== current) return;
        // The choice's effect observes it; the flag keeps that to one retry.
        retrying = true;
        setChoice(fresh?.targets.find((entry) => entry.targetId === target.targetId));
        return;
      }
      setProblem(
        changesProblem(failure) ?? describe({ fail }, failure, "The changes couldn’t be read.") ?? "",
      );
    } finally {
      if (controller === current) setLoading(false);
    }
  };

  const loadMore = async () => {
    const current = observed();
    if (!current?.nextCursor || loadingMore()) return;
    setLoadingMore(true);
    try {
      const result = await page(choice(), new AbortController().signal, current.nextCursor, current.revision);
      if (observed() !== current) return;
      setObserved({ ...current, files: [...current.files, ...result.files], nextCursor: result.nextCursor });
    } catch (failure) {
      if (movedOn(failure)) void observe();
      else if (!fail(failure)) setProblem(describe({ fail }, failure, "More files couldn’t be read.") ?? "");
    } finally {
      setLoadingMore(false);
    }
  };

  void loadCatalog();
  createEffect(
    on(choice, () => {
      const retried = retrying;
      retrying = false;
      void observe(retried);
    }),
  );
  // A turn that ends may have changed files; so may time away from the page.
  createEffect(
    on(
      () => props.session.activeTurnId,
      (turn, previous) => {
        if (previous && !turn) void observe();
      },
      { defer: true },
    ),
  );
  const visible = () => {
    if (document.visibilityState === "visible") void observe();
  };
  document.addEventListener("visibilitychange", visible);
  onCleanup(() => document.removeEventListener("visibilitychange", visible));

  const review: Review = {
    api: props.api,
    sessionId: id(),
    targetId: () => observed()?.targetId ?? "",
    revision: () => observed()?.revision ?? "",
    generation,
    findComment: (id) => props.session.comments.find((comment) => comment.id === id),
    moveFailed,
    reportMoveFailure: setMoveFailed,
    selection,
    setSelection(value) {
      setEditing(undefined);
      setSelection(value);
    },
    clearSelection: () => setSelection(undefined),
    replacing,
    setReplacing,
    editing,
    setEditing(value) {
      if (value !== undefined) setSelection(undefined);
      setEditing(value);
    },
    reobserve: () => void observe(),
    openFile: (path) => props.onOpenFile(path),
    refreshComments,
    fail,
  };

  // Comments on this target's files go with their file; the rest are listed first.
  const byFile = createMemo(() => {
    const current = observed();
    const paths = new Set(current?.files.map((file) => file.path));
    const files = new Map<string, Comment[]>();
    const elsewhere: Comment[] = [];
    for (const comment of props.session.comments) {
      if (current && comment.diff?.targetId === current.targetId && paths.has(comment.path)) {
        files.set(comment.path, [...(files.get(comment.path) ?? []), comment]);
      } else {
        elsewhere.push(comment);
      }
    }
    return { files, elsewhere };
  });

  const files = createMemo(() => new Map(observed()?.files.map((file) => [file.path, file])));
  const paths = createMemo(() => observed()?.files.map((file) => file.path) ?? [], undefined, {
    equals: (a, b) => a.length === b.length && a.every((value, index) => value === b[index]),
  });

  const summary = createMemo(() => {
    const current = observed();
    return current ? summarize(current.files, Boolean(current.nextCursor)) : undefined;
  });

  const toggle = (path: string) => {
    const next = new Set(open());
    if (next.has(path)) next.delete(path);
    else next.add(path);
    setOpen(next);
  };

  // A stale comment being moved opens its file, where its new lines are picked.
  createEffect(
    on(replacing, (comment) => {
      if (comment && !open().has(comment.path)) setOpen(new Set([...open(), comment.path]));
    }),
  );

  return (
    <>
      <header class="bar">
        <button
          type="button"
          class="icon-button"
          aria-label="Back to the conversation"
          onClick={props.onClose}
        >
          <Icon name="back" />
        </button>
        <div class="bar-title">
          <h1>Changes</h1>
          <p class="muted">{props.session.info.name || "Untitled session"}</p>
        </div>
        <TargetPicker
          catalog={catalog()}
          error={catalogError()}
          current={choice()}
          onPick={(entry) => {
            setSelection(undefined);
            setReplacing(undefined);
            setChoice(entry.kind === "working_tree" ? undefined : entry);
          }}
          onOpen={() => void loadCatalog()}
        />
      </header>
      <div class="changes">
        <Switch>
          <Match when={problem()}>
            <div class="empty-state">
              <p>{problem()}</p>
              <button type="button" onClick={() => observe()}>
                Try again
              </button>
            </div>
          </Match>
          <Match when={!observed() && loading()}>
            <p class="empty">
              <Spinner /> Reading changes…
            </p>
          </Match>
          <Match when={summary()}>
            {(value) => (
              <>
                <p class="changes-summary">
                  <Show when={value().files > 0} fallback={<span class="muted">No changes</span>}>
                    <span class="muted">{filesLabel(value())} changed</span>
                    <Show when={value().counts}>
                      {(counts) => (
                        <>
                          <span class="mono added">+{counts().additions}</span>
                          <span class="mono removed">−{counts().deletions}</span>
                        </>
                      )}
                    </Show>
                  </Show>
                  <Show when={loading()}>
                    <Spinner />
                  </Show>
                </p>
                <Show when={byFile().elsewhere.length > 0}>
                  <section class="changes-elsewhere" aria-label="Comments on other changes">
                    <p class="eyebrow">Comments on other changes</p>
                    <For each={byFile().elsewhere}>
                      {(comment) => <Note desk={review} comment={comment} showPath />}
                    </For>
                  </section>
                </Show>
                {/* Keyed by path, so a file keeps its place and state when the changes are read again. */}
                <For each={paths()}>
                  {(path) => (
                    <Show when={files().get(path)}>
                      {(file) => (
                        <FileDiff
                          review={review}
                          file={file()}
                          open={open().has(path)}
                          onToggle={() => toggle(path)}
                          comments={byFile().files.get(path) ?? []}
                        />
                      )}
                    </Show>
                  )}
                </For>
                <Show when={observed()?.nextCursor}>
                  <button type="button" class="diff-more" disabled={loadingMore()} onClick={loadMore}>
                    {loadingMore() ? "Loading…" : "Show more files"}
                  </button>
                </Show>
              </>
            )}
          </Match>
        </Switch>
      </div>
    </>
  );
}

function TargetPicker(props: {
  catalog: DiffTargetCatalog | undefined;
  error: string;
  current: DiffTargetEntry | undefined;
  onPick(entry: DiffTargetEntry): void;
  onOpen(): void;
}) {
  const [query, setQuery] = createSignal("");
  let popover: HTMLDivElement | undefined;
  let search: HTMLInputElement | undefined;
  const isCurrent = (entry: DiffTargetEntry) =>
    props.current ? props.current.targetId === entry.targetId : entry.kind === "working_tree";
  const matches = (entry: DiffTargetEntry) => {
    const words = query().trim().toLowerCase();
    if (!words) return true;
    const { metadata } = entry;
    return [metadata.label, metadata.subject, metadata.abbreviatedOid, metadata.refName, metadata.baseRefName]
      .filter(Boolean)
      .some((value) => value?.toLowerCase().includes(words));
  };
  const targets = () => (props.catalog?.targets ?? []).filter(matches);
  const top = () => targets().filter((entry) => entry.kind !== "commit");
  const commits = () => targets().filter((entry) => entry.kind === "commit");
  const pick = (entry: DiffTargetEntry) => {
    popover?.hidePopover();
    props.onPick(entry);
  };
  const count = (entry: DiffTargetEntry) => entry.annotationCount ?? 0;

  return (
    <>
      <button type="button" class="target-button" popovertarget="target-picker">
        <span>{targetButtonLabel(props.current)}</span>
        <Icon name="chevDown" size={16} />
      </button>
      <div
        id="target-picker"
        popover
        class="menu sheet-menu target-menu"
        ref={popover}
        onToggle={(event) => {
          if ((event as ToggleEvent).newState !== "open") return;
          setQuery("");
          props.onOpen();
          // A phone's keyboard would cover the list, so only a pointer opens it focused.
          if (matchMedia("(pointer: fine)").matches) requestAnimationFrame(() => search?.focus());
        }}
      >
        <label class="menu-search">
          <Icon name="search" size={18} />
          <span class="visually-hidden">Search commits and branches</span>
          <input
            ref={search}
            type="search"
            placeholder="Search commits and branches"
            value={query()}
            onInput={(event) => setQuery(event.currentTarget.value)}
          />
        </label>
        <Show
          when={props.catalog}
          fallback={<p class="muted small menu-note">{props.error || "Loading…"}</p>}
        >
          <div class="menu-scroll">
            <For each={top()}>
              {(entry) => (
                <button
                  type="button"
                  class="menu-row target-row"
                  aria-current={isCurrent(entry) || undefined}
                  onClick={() => pick(entry)}
                >
                  <span class="target-check">
                    <Show when={isCurrent(entry)}>
                      <Icon name="check" size={16} />
                    </Show>
                  </span>
                  <span class="grow">
                    <span>{entry.kind === "working_tree" ? "Uncommitted changes" : "This branch"}</span>
                    <Show when={entry.kind === "branch"}>
                      <span class="mono muted small target-refs">
                        {entry.metadata.refName}
                        <Icon name="arrowRight" size={12} />
                        {entry.metadata.baseRefName}
                      </span>
                    </Show>
                  </span>
                  <Show when={count(entry) > 0}>
                    <span class="muted small">{commentCount(count(entry))}</span>
                  </Show>
                </button>
              )}
            </For>
            <Show when={commits().length > 0}>
              <hr />
              <span class="eyebrow menu-group">Commits</span>
              <For each={commits()}>
                {(entry) => (
                  <button
                    type="button"
                    class="menu-row target-row"
                    aria-current={isCurrent(entry) || undefined}
                    onClick={() => pick(entry)}
                  >
                    <span class="target-check">
                      <Show when={isCurrent(entry)}>
                        <Icon name="check" size={16} />
                      </Show>
                    </span>
                    <span class="grow">
                      <span class="target-subject">{entry.metadata.subject || entry.metadata.label}</span>
                      <span class="mono muted small">
                        {[
                          shortOid(entry.metadata.abbreviatedOid),
                          entry.metadata.committedAt
                            ? relativeTime(new Date(entry.metadata.committedAt * 1000).toISOString())
                            : "",
                        ]
                          .filter(Boolean)
                          .join(" · ")}
                      </span>
                    </span>
                    <Show when={count(entry) > 0}>
                      <span class="muted small">{commentCount(count(entry))}</span>
                    </Show>
                  </button>
                )}
              </For>
            </Show>
            <Show when={targets().length === 0}>
              <p class="muted small menu-note">Nothing matches.</p>
            </Show>
          </div>
        </Show>
      </div>
    </>
  );
}
