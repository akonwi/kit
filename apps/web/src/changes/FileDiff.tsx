// One changed file: its header, which opens and closes it, and its hunks with
// the comments on them. A tap on a line number picks it; a tap on a second
// extends to it, and an editor under the lines takes the comment.
import { type Accessor, createEffect, createMemo, createSignal, For, on, onCleanup, Show } from "solid-js";
import { isAbort } from "../api/client";
import type { DiffFileSummary } from "../api/types";
import { Icon, Spinner } from "../ui/icons";
import type { Comment } from "./comments";
import {
  appendHunks,
  type DiffRow,
  fileCounts,
  fileTag,
  type HunkView,
  type LineCounts,
  readable,
  rowFor,
  type Selection,
  selected,
  selectionProblem,
  selectionRange,
  shownLine,
  splitPath,
  tapLine,
  unreadableReason,
} from "./diff";
import { type CommentDesk, CommentEditor, describe, movedOn, Note } from "./Note";

export { describe, movedOn, Note } from "./Note";

/** What every file shares: the session, the observed target, and the one selection. */
export interface Review extends CommentDesk {
  targetId: Accessor<string>;
  revision: Accessor<string>;
  /** Counts the target's observations; one may renew the same revision. */
  generation: Accessor<number>;
  selection: Accessor<Selection | undefined>;
  setSelection(selection: Selection | undefined): void;
  /** Reads the changes again after the server said they moved on. */
  reobserve(): void;
  /** Opens the file as it is now, when the screen can. */
  openFile?(path: string): void;
}

export function FileDiff(props: {
  review: Review;
  file: DiffFileSummary;
  open: boolean;
  onToggle(): void;
  /** This file's comments in this target. */
  comments: Comment[];
}) {
  const review = () => props.review;
  const file = () => props.file;
  const [hunks, setHunks] = createSignal<HunkView[]>([]);
  const [next, setNext] = createSignal<string>();
  const [loading, setLoading] = createSignal(false);
  const [error, setError] = createSignal("");
  const [tooComplex, setTooComplex] = createSignal(false);
  let controller: AbortController | undefined;
  onCleanup(() => controller?.abort());
  // A read the server refused as expired waits for the next observation,
  // which may renew the same revision.
  let expired = false;

  const load = async (cursor?: string) => {
    controller?.abort();
    const current = new AbortController();
    controller = current;
    setLoading(true);
    setError("");
    try {
      const page = await review().api.readFileDiff(
        review().sessionId,
        {
          targetId: review().targetId(),
          targetRevision: review().revision(),
          path: file().path,
          ...(file().fileRevision ? { expectedFileRevision: file().fileRevision } : {}),
          highlight: true,
          ...(cursor ? { cursor } : {}),
        },
        current.signal,
      );
      if (controller !== current) return;
      setHunks(cursor ? appendHunks(hunks(), page) : appendHunks([], page));
      setNext(page.nextCursor);
      setTooComplex(page.computation.state === "too_complex");
    } catch (failure) {
      if (isAbort(failure) || controller !== current) return;
      if (movedOn(failure)) {
        expired = true;
        review().reobserve();
        return;
      }
      const message = describe(review(), failure, "The changes couldn’t be read.");
      if (message) setError(message);
    } finally {
      if (controller === current) setLoading(false);
    }
  };

  // An open file reads its hunks, and reads them again when its changes do,
  // or after a read the server refused as expired.
  createEffect(
    on(
      () => [props.open, review().revision(), file().fileRevision, review().generation()] as const,
      ([open, revision, fileRevision], previous) => {
        if (!open || !readable(file())) return;
        const same = previous?.[0] && previous[1] === revision && previous[2] === fileRevision;
        if (same && !expired) return;
        expired = false;
        void load();
      },
    ),
  );

  const rows = createMemo(() => hunks().flatMap((hunk) => hunk.rows));
  const starts = createMemo(() => {
    let at = 0;
    return hunks().map((hunk) => {
      const start = at;
      at += hunk.rows.length;
      return start;
    });
  });

  // Each comment goes under the last of its lines that is shown; one whose
  // lines aren't shown, or whose source changed, goes above the hunks.
  const placed = createMemo(() => {
    const after = new Map<number, Comment[]>();
    const above: Comment[] = [];
    for (const comment of props.comments) {
      const at =
        comment.stale || !comment.diff
          ? -1
          : rowFor(rows(), comment.diff.side as "old" | "new", comment.startLine, comment.endLine);
      if (at < 0) above.push(comment);
      else after.set(at, [...(after.get(at) ?? []), comment]);
    }
    return { after, above };
  });

  const selection = () => {
    const value = review().selection();
    return value?.path === file().path ? value : undefined;
  };
  const editorAt = createMemo(() => {
    const value = selection();
    if (!value) return -1;
    const { start, end } = selectionRange(value);
    return rowFor(rows(), value.side, start, end);
  });

  // The server may count a file's lines; otherwise they're counted once all its hunks are read.
  const counts = createMemo(() =>
    fileCounts(file(), hunks(), hunks().length > 0 && !next() && !tooComplex()),
  );
  const tag = () => fileTag(file());
  const path = () => splitPath(file().path);
  const canOpen = () => readable(file());
  // The file as it is now; a removed one has none to open.
  const openFile = () =>
    canOpen() && file().change !== "deleted" ? props.review.openFile?.bind(props.review) : undefined;

  return (
    <section class="diff-file" aria-label={file().path}>
      <div class="diff-file-bar" data-open={(props.open && canOpen()) || undefined}>
        <button
          type="button"
          class="diff-file-head"
          aria-expanded={canOpen() ? props.open : undefined}
          aria-disabled={canOpen() ? undefined : "true"}
          onClick={() => canOpen() && props.onToggle()}
        >
          <Icon name={props.open ? "chevDown" : "chevRight"} size={16} />
          <span class="diff-path mono grow">
            <span class="muted">{path().dir}</span>
            <strong>{path().name}</strong>
          </span>
          <Show when={tag()}>
            <span class="diff-tag">{tag()}</span>
          </Show>
          <Counts counts={counts()} />
        </button>
        <Show when={openFile()}>
          {(open) => (
            <button
              type="button"
              class="icon-button small diff-open"
              aria-label={`Open ${path().name}`}
              title="Open file"
              onClick={() => open()(file().path)}
            >
              <Icon name="file" size={16} />
            </button>
          )}
        </Show>
      </div>
      <Show when={!canOpen() && props.comments.length > 0}>
        <div class="diff-body">
          <p class="diff-note muted small">{unreadableReason(file())}</p>
          <For each={props.comments}>
            {(comment) => <Note desk={review()} comment={comment} canMove={canOpen()} />}
          </For>
        </div>
      </Show>
      <Show when={props.open && canOpen()}>
        <div class="diff-body">
          <For each={placed().above}>
            {(comment) => <Note desk={review()} comment={comment} canMove={canOpen()} />}
          </For>
          <Show when={tooComplex()}>
            <p class="diff-note muted small">These changes are too complex to show line by line.</p>
          </Show>
          <Show when={error()}>
            <p class="diff-note error small" role="alert">
              {error()}{" "}
              <button type="button" class="link" onClick={() => load()}>
                Retry
              </button>
            </p>
          </Show>
          <Show when={loading() && hunks().length === 0}>
            <p class="diff-note muted small">
              <Spinner /> Loading…
            </p>
          </Show>
          <Show when={!loading() && !error() && !tooComplex() && hunks().length === 0}>
            <p class="diff-note muted small">
              {file().change === "mode_changed" ? "Only the file’s mode changed." : "No line changes."}
            </p>
          </Show>
          <For each={hunks()}>
            {(hunk, index) => (
              <div class="diff-hunk">
                <div class="diff-hunk-head mono">{hunk.header}</div>
                <For each={hunk.rows}>
                  {(row, position) => {
                    const at = () => (starts()[index()] ?? 0) + position();
                    return (
                      <>
                        <Line review={review()} file={file()} row={row} />
                        <For each={placed().after.get(at())}>
                          {(comment) => <Note desk={review()} comment={comment} canMove={canOpen()} />}
                        </For>
                        <Show when={editorAt() === at() && selection()}>
                          {(value) => (
                            <Editor review={review()} file={file()} selection={value()} rows={rows()} />
                          )}
                        </Show>
                      </>
                    );
                  }}
                </For>
              </div>
            )}
          </For>
          <Show when={next()}>
            {(cursor) => (
              <button type="button" class="diff-more" disabled={loading()} onClick={() => load(cursor())}>
                {loading() ? "Loading…" : "Show more changes"}
              </button>
            )}
          </Show>
        </div>
      </Show>
    </section>
  );
}

function Counts(props: { counts: LineCounts | undefined }) {
  return (
    <Show when={props.counts}>
      {(counts) => (
        <span class="diff-counts mono">
          <Show when={counts().additions > 0}>
            <span class="added">+{counts().additions}</span>
          </Show>
          <Show when={counts().deletions > 0}>
            <span class="removed">−{counts().deletions}</span>
          </Show>
        </span>
      )}
    </Show>
  );
}

function Line(props: { review: Review; file: DiffFileSummary; row: DiffRow }) {
  const row = () => props.row;
  const isSelected = () => selected(props.review.selection(), props.file.path, row());
  const number = () => shownLine(row());
  const commentable = () => Boolean(props.file.fileRevision);
  return (
    <div class="diff-row" data-kind={row().kind} data-selected={isSelected() || undefined}>
      <button
        type="button"
        class="diff-num mono"
        disabled={!commentable()}
        aria-pressed={isSelected()}
        aria-label={`${row().kind === "deletion" ? "Removed line" : "Line"} ${number()}`}
        onClick={() => props.review.setSelection(tapLine(props.review.selection(), props.file.path, row()))}
      >
        {number()}
      </button>
      <span class="diff-sign mono" aria-hidden="true">
        {row().kind === "addition" ? "+" : row().kind === "deletion" ? "-" : ""}
      </span>
      <span class="diff-code">
        <Show when={row().runs} fallback={row().content}>
          {(runs) => (
            <For each={runs()}>
              {(run) => (run.tone ? <span class={`hl-${run.tone}`}>{run.text}</span> : run.text)}
            </For>
          )}
        </Show>
      </span>
    </div>
  );
}

/** The editor for a new comment on the selected lines, or one moved from stale lines. */
function Editor(props: { review: Review; file: DiffFileSummary; selection: Selection; rows: DiffRow[] }) {
  const range = () => selectionRange(props.selection);
  return (
    <CommentEditor
      desk={props.review}
      start={range().start}
      end={range().end}
      problem={selectionProblem(props.selection, props.rows)}
      anchor={() => {
        const fileRevision = props.file.fileRevision;
        if (!fileRevision) return undefined;
        return {
          kind: "working_tree_diff",
          workingTreeDiff: {
            targetId: props.review.targetId(),
            targetRevision: props.review.revision(),
            path: props.file.path,
            fileRevision,
            side: props.selection.side,
            startLine: range().start,
            endLine: range().end,
          },
        };
      }}
      onMovedOn={() => props.review.reobserve()}
    />
  );
}
