// A comment among the lines it's on, wherever lines are drawn: Changes or a
// file. Its words, edit and delete, and for a stale one, a new range or
// delete. The editor takes a new comment, or one moved from stale lines.
import { type Accessor, createEffect, createSignal, Show } from "solid-js";
import { type Api, ApiError } from "../api/client";
import type { AnnotationAnchor } from "../api/types";
import { Icon } from "../ui/icons";
import { type Comment, linesLabel, MAX_COMMENT_BYTES, staleLabel } from "./comments";

/** What a screen of comments shares: the session, and which comment is edited or moved. */
export interface CommentDesk {
  api: Api;
  sessionId: string;
  /** A comment as the session has it now. */
  findComment(id: number): Comment | undefined;
  /** Clears the lines picked for a new comment. */
  clearSelection(): void;
  /** The stale comment the selection will move, if any. */
  replacing: Accessor<Comment | undefined>;
  setReplacing(comment: Comment | undefined): void;
  /** The comment being edited in place. */
  editing: Accessor<number | undefined>;
  setEditing(id: number | undefined): void;
  /** A comment moved to new lines whose old copy wasn't deleted. */
  moveFailed: Accessor<number | undefined>;
  reportMoveFailure(id: number | undefined): void;
  /** Reads the comments again, for their whole text. */
  refreshComments(): Promise<void>;
  fail(error: unknown): boolean;
}

/** A failure in words, or undefined when the app took it over. */
export function describe(
  desk: Pick<CommentDesk, "fail">,
  error: unknown,
  fallback: string,
): string | undefined {
  if (desk.fail(error)) return undefined;
  return error instanceof Error && error.message ? error.message : fallback;
}

/** Whether the server refused because what was read moved on since. */
export function movedOn(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    ["stale_target", "stale_file", "stale_cursor", "stale_workspace"].includes(error.code)
  );
}

const encoder = new TextEncoder();

const MOVE_FAILED =
  "The comment moved to its new lines, but this copy wasn’t deleted. Delete it before sending.";

/** The editor for a new comment on picked lines, or one moved from stale lines. */
export function CommentEditor(props: {
  desk: CommentDesk;
  start: number;
  end: number;
  /** Why these lines can't take a comment, if they can't. */
  problem?: string;
  /** Where the comment goes, or undefined when it can't be placed. */
  anchor(): AnnotationAnchor | undefined;
  /** The source moved on since it was read; it's read again. */
  onMovedOn(): void;
}) {
  const desk = () => props.desk;
  const moving = () => desk().replacing();
  const [body, setBody] = createSignal(moving()?.body ?? "");
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");
  const tooLong = () => encoder.encode(body()).byteLength > MAX_COMMENT_BYTES;
  let field: HTMLTextAreaElement | undefined;
  createEffect(() => field?.focus({ preventScroll: false }));

  const cancel = () => {
    desk().clearSelection();
    desk().setReplacing(undefined);
  };

  const save = async () => {
    const text = body().trim();
    const anchor = props.anchor();
    if (!text || busy() || props.problem || tooLong() || !anchor) return;
    setBusy(true);
    setError("");
    try {
      await desk().api.createAnnotation(desk().sessionId, { anchor, body: text });
      // The new comment is saved; the old one is only removed, so a failure
      // here leaves it to delete from its note rather than saving twice.
      const old = moving();
      desk().setReplacing(undefined);
      desk().clearSelection();
      if (old) {
        try {
          await desk().api.deleteAnnotation(desk().sessionId, old.id);
        } catch (failure) {
          if (!desk().fail(failure)) desk().reportMoveFailure(old.id);
        }
      }
    } catch (failure) {
      if (movedOn(failure)) {
        setError("The file changed since it was read. Select the lines again.");
        props.onMovedOn();
        return;
      }
      const message = describe(desk(), failure, "The comment wasn’t saved.");
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
        {moving() ? "Move comment to" : "Comment on"} {linesLabel(props.start, props.end).toLowerCase()}
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
      <Show when={props.problem ?? (tooLong() ? "This comment is too long." : error())}>
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
          disabled={!body().trim() || busy() || Boolean(props.problem) || tooLong()}
        >
          {busy() ? "Saving…" : moving() ? "Move" : "Comment"}
        </button>
      </div>
    </form>
  );
}

/** A saved comment among the lines: its lines, its words, and edit and delete. */
export function Note(props: {
  desk: CommentDesk;
  comment: Comment;
  /** Whether its lines are drawn here, so a stale one can move to new ones. */
  canMove?: boolean;
  showPath?: boolean;
}) {
  const desk = () => props.desk;
  const comment = () => props.comment;
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");
  const editing = () => desk().editing() === comment().id;
  const moving = () => desk().replacing()?.id === comment().id;
  const where = () =>
    `${props.showPath ? `${comment().path} · ` : ""}${linesLabel(comment().startLine, comment().endLine)}`;

  const remove = async () => {
    setBusy(true);
    setError("");
    try {
      await desk().api.deleteAnnotation(desk().sessionId, comment().id);
      if (moving()) desk().setReplacing(undefined);
      if (desk().moveFailed() === comment().id) desk().reportMoveFailure(undefined);
    } catch (failure) {
      const message = describe(desk(), failure, "The comment wasn’t deleted.");
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
        await desk().refreshComments();
      } catch (failure) {
        const message = describe(desk(), failure, "The comment couldn’t be read.");
        if (message) setError(message);
        return undefined;
      }
    }
    const current = desk().findComment(id);
    if (current && !current.complete) setError("The comment couldn’t be read in full.");
    return current?.complete ? current : undefined;
  };

  const edit = async () => {
    const current = await whole();
    if (current) desk().setEditing(current.id);
  };

  const moveTo = async () => {
    const current = await whole();
    if (!current) return;
    desk().clearSelection();
    desk().setReplacing(current);
  };

  return (
    <Show when={!editing()} fallback={<EditNote desk={desk()} comment={comment()} />}>
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
                <button type="button" class="link" onClick={() => desk().setReplacing(undefined)}>
                  Cancel
                </button>
              </div>
            }
          >
            <div class="comment-actions start">
              <Show when={props.canMove}>
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
        <Show when={error() || (desk().moveFailed() === comment().id && MOVE_FAILED)}>
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

function EditNote(props: { desk: CommentDesk; comment: Comment }) {
  const desk = () => props.desk;
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
      desk().setEditing(undefined);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await desk().api.updateAnnotation(desk().sessionId, props.comment.id, text);
      desk().setEditing(undefined);
    } catch (failure) {
      const message = describe(desk(), failure, "The comment wasn’t saved.");
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
          if (event.key === "Escape") desk().setEditing(undefined);
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
        <button type="button" onClick={() => desk().setEditing(undefined)}>
          Cancel
        </button>
        <button type="submit" data-variant="primary" disabled={!body().trim() || busy() || tooLong()}>
          {busy() ? "Saving…" : "Save"}
        </button>
      </div>
    </form>
  );
}
