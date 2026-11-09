// One changed file: its header, which opens and closes it, and its hunks with
// the comments on them. A tap on a line number picks it; a tap on a second
// extends to it, and an editor under the lines takes the comment.
import { type Accessor, createEffect, createMemo, createSignal, For, on, onCleanup, Show } from "solid-js";
import { type Api, ApiError, isAbort } from "../api/client";
import type { DiffFileSummary } from "../api/types";
import { Icon, Spinner } from "../ui/icons";
import { type Comment, linesLabel, MAX_COMMENT_BYTES, staleLabel } from "./comments";
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

/** What every file shares: the session, the observed target, and the one selection. */
export interface Review {
  api: Api;
  sessionId: string;
  targetId: Accessor<string>;
  revision: Accessor<string>;
  /** Counts the target's observations; one may renew the same revision. */
  generation: Accessor<number>;
  /** A comment as the session has it now. */
  findComment(id: number): Comment | undefined;
  selection: Accessor<Selection | undefined>;
  setSelection(selection: Selection | undefined): void;
  /** The stale comment the selection will move, if any. */
  replacing: Accessor<Comment | undefined>;
  setReplacing(comment: Comment | undefined): void;
  /** The comment being edited in place. */
  editing: Accessor<number | undefined>;
  setEditing(id: number | undefined): void;
  /** Reads the changes again after the server said they moved on. */
  reobserve(): void;
  /** A comment moved to new lines whose old copy wasn't deleted. */
  moveFailed: Accessor<number | undefined>;
  reportMoveFailure(id: number | undefined): void;
  /** Reads the comments again, for their whole text. */
  refreshComments(): Promise<void>;
  fail(error: unknown): boolean;
}

/** A failure in words, or undefined when the app took it over. */
export function describe(review: Pick<Review, "fail">, error: unknown, fallback: string): string | undefined {
  if (review.fail(error)) return undefined;
  return error instanceof Error && error.message ? error.message : fallback;
}

/** Whether the server refused because the changes moved on since they were read. */
export function movedOn(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    ["stale_target", "stale_file", "stale_cursor", "stale_workspace"].includes(error.code)
  );
}

const encoder = new TextEncoder();

const MOVE_FAILED =
  "The comment moved to its new lines, but this copy wasn’t deleted. Delete it before sending.";

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

  return (
    <section class="diff-file" aria-label={file().path}>
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
      <Show when={!canOpen() && props.comments.length > 0}>
        <div class="diff-body">
          <p class="diff-note muted small">{unreadableReason(file())}</p>
          <For each={props.comments}>
            {(comment) => <Note review={review()} comment={comment} file={file()} />}
          </For>
        </div>
      </Show>
      <Show when={props.open && canOpen()}>
        <div class="diff-body">
          <For each={placed().above}>
            {(comment) => <Note review={review()} comment={comment} file={file()} />}
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
                          {(comment) => <Note review={review()} comment={comment} file={file()} />}
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
  const review = () => props.review;
  const moving = () => review().replacing();
  const [body, setBody] = createSignal(moving()?.body ?? "");
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");
  const range = () => selectionRange(props.selection);
  const problem = () => selectionProblem(props.selection, props.rows);
  const tooLong = () => encoder.encode(body()).byteLength > MAX_COMMENT_BYTES;
  let field: HTMLTextAreaElement | undefined;
  createEffect(() => field?.focus({ preventScroll: false }));

  const cancel = () => {
    review().setSelection(undefined);
    review().setReplacing(undefined);
  };

  const save = async () => {
    const text = body().trim();
    if (!text || busy() || problem() || tooLong() || !props.file.fileRevision) return;
    setBusy(true);
    setError("");
    try {
      await review().api.createAnnotation(review().sessionId, {
        anchor: {
          kind: "working_tree_diff",
          workingTreeDiff: {
            targetId: review().targetId(),
            targetRevision: review().revision(),
            path: props.file.path,
            fileRevision: props.file.fileRevision,
            side: props.selection.side,
            startLine: range().start,
            endLine: range().end,
          },
        },
        body: text,
      });
      // The new comment is saved; the old one is only removed, so a failure
      // here leaves it to delete from its note rather than saving twice.
      const old = moving();
      review().setReplacing(undefined);
      review().setSelection(undefined);
      if (old) {
        try {
          await review().api.deleteAnnotation(review().sessionId, old.id);
        } catch (failure) {
          if (!review().fail(failure)) review().reportMoveFailure(old.id);
        }
      }
    } catch (failure) {
      if (movedOn(failure)) {
        setError("The file changed since it was read. Select the lines again.");
        review().reobserve();
        return;
      }
      const message = describe(review(), failure, "The comment wasn’t saved.");
      if (message) setError(message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      class="comment-editor"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <label for="comment-body" class="muted small">
        {moving() ? "Move comment to" : "Comment on"} {linesLabel(range().start, range().end).toLowerCase()}
      </label>
      <textarea
        id="comment-body"
        ref={field}
        rows={2}
        value={body()}
        placeholder="What should change here?"
        readOnly={busy()}
        onInput={(event) => setBody(event.currentTarget.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            void save();
          }
          if (event.key === "Escape") cancel();
        }}
      />
      <Show when={problem() ?? (tooLong() ? "This comment is too long." : error())}>
        {(message) => (
          <p class="error small" role="alert">
            {message()}
          </p>
        )}
      </Show>
      <div class="comment-actions">
        <button type="button" onClick={cancel}>
          Cancel
        </button>
        <button
          type="submit"
          data-variant="primary"
          disabled={!body().trim() || busy() || Boolean(problem()) || tooLong()}
        >
          {busy() ? "Saving…" : moving() ? "Move" : "Comment"}
        </button>
      </div>
    </form>
  );
}

/** A saved comment among the lines: its lines, its words, and edit and delete. */
export function Note(props: {
  review: Review;
  comment: Comment;
  file?: DiffFileSummary;
  showPath?: boolean;
}) {
  const review = () => props.review;
  const comment = () => props.comment;
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");
  const editing = () => review().editing() === comment().id;
  const moving = () => review().replacing()?.id === comment().id;
  const where = () =>
    `${props.showPath ? `${comment().path} · ` : ""}${linesLabel(comment().startLine, comment().endLine)}`;

  const remove = async () => {
    setBusy(true);
    setError("");
    try {
      await review().api.deleteAnnotation(review().sessionId, comment().id);
      if (moving()) review().setReplacing(undefined);
      if (review().moveFailed() === comment().id) review().reportMoveFailure(undefined);
    } catch (failure) {
      const message = describe(review(), failure, "The comment wasn’t deleted.");
      if (message) setError(message);
    } finally {
      setBusy(false);
    }
  };

  /** The whole comment as the session has it now; a snapshot carries only its start. */
  const whole = async (): Promise<Comment | undefined> => {
    setError("");
    const id = comment().id;
    if (!comment().complete) {
      try {
        await review().refreshComments();
      } catch (failure) {
        const message = describe(review(), failure, "The comment couldn’t be read.");
        if (message) setError(message);
        return undefined;
      }
    }
    const current = review().findComment(id);
    if (current && !current.complete) setError("The comment couldn’t be read in full.");
    return current?.complete ? current : undefined;
  };

  const edit = async () => {
    const current = await whole();
    if (current) review().setEditing(current.id);
  };

  const moveTo = async () => {
    const current = await whole();
    if (!current) return;
    review().setSelection(undefined);
    review().setReplacing(current);
  };

  return (
    <Show when={!editing()} fallback={<EditNote review={review()} comment={comment()} />}>
      <div class="comment-note" data-stale={comment().stale || undefined}>
        <div class="comment-note-head">
          <Icon name={comment().stale ? "alert" : "comment"} size={16} />
          <span class="grow">
            {comment().stale ? staleLabel(comment()) : `${where()} · goes with your next message`}
          </span>
          <Show when={!comment().stale}>
            <button
              type="button"
              class="icon-button small"
              aria-label="Edit comment"
              disabled={busy()}
              onClick={edit}
            >
              <Icon name="edit" size={18} />
            </button>
            <button
              type="button"
              class="icon-button small"
              aria-label="Delete comment"
              disabled={busy()}
              onClick={remove}
            >
              <Icon name="trash" size={18} />
            </button>
          </Show>
        </div>
        <p class="comment-body">{comment().body}</p>
        <Show when={comment().stale}>
          <Show
            when={!moving()}
            fallback={
              <div class="comment-actions start">
                <span class="muted small grow">Tap the line numbers this comment is about.</span>
                <button type="button" class="link" onClick={() => review().setReplacing(undefined)}>
                  Cancel
                </button>
              </div>
            }
          >
            <div class="comment-actions start">
              <Show when={props.file && readable(props.file)}>
                <button type="button" disabled={busy()} onClick={moveTo}>
                  Select new range
                </button>
              </Show>
              <button type="button" class="link danger" disabled={busy()} onClick={remove}>
                Delete
              </button>
            </div>
          </Show>
        </Show>
        <Show when={error() || (review().moveFailed() === comment().id && MOVE_FAILED)}>
          {(message) => (
            <p class="error small" role="alert">
              {message()}
            </p>
          )}
        </Show>
      </div>
    </Show>
  );
}

function EditNote(props: { review: Review; comment: Comment }) {
  const review = () => props.review;
  const [body, setBody] = createSignal(props.comment.body);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");
  const tooLong = () => encoder.encode(body()).byteLength > MAX_COMMENT_BYTES;
  let field: HTMLTextAreaElement | undefined;
  createEffect(() => field?.focus());
  const save = async () => {
    const text = body().trim();
    if (!text || busy() || tooLong()) return;
    if (text === props.comment.body) {
      review().setEditing(undefined);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await review().api.updateAnnotation(review().sessionId, props.comment.id, text);
      review().setEditing(undefined);
    } catch (failure) {
      const message = describe(review(), failure, "The comment wasn’t saved.");
      if (message) setError(message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      class="comment-editor"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <label for={`comment-edit-${props.comment.id}`} class="muted small">
        Edit comment on {linesLabel(props.comment.startLine, props.comment.endLine).toLowerCase()}
      </label>
      <textarea
        id={`comment-edit-${props.comment.id}`}
        ref={field}
        rows={2}
        value={body()}
        readOnly={busy()}
        onInput={(event) => setBody(event.currentTarget.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            void save();
          }
          if (event.key === "Escape") review().setEditing(undefined);
        }}
      />
      <Show when={tooLong() ? "This comment is too long." : error()}>
        {(message) => (
          <p class="error small" role="alert">
            {message()}
          </p>
        )}
      </Show>
      <div class="comment-actions">
        <button type="button" onClick={() => review().setEditing(undefined)}>
          Cancel
        </button>
        <button type="submit" data-variant="primary" disabled={!body().trim() || busy() || tooLong()}>
          {busy() ? "Saving…" : "Save"}
        </button>
      </div>
    </form>
  );
}
