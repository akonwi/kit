// A file in the session's folder, read only: its lines in the server's
// colors, the lines asked for marked and in view, and comments on lines as
// on Changes. Markdown opens as a preview, with its source one tap away.
import {
  batch,
  createEffect,
  createMemo,
  createSignal,
  For,
  Match,
  on,
  onCleanup,
  Show,
  Switch,
} from "solid-js";
import { type Api, isAbort } from "../api/client";
import type { WorkspaceFileRead } from "../api/types";
import type { Comment } from "../changes/comments";
import { type CommentDesk, CommentEditor, describe, Note } from "../changes/Note";
import { Markdown } from "../markdown/Markdown";
import type { SessionState } from "../session/projection";
import { copyText } from "../ui/copy";
import { Icon, Spinner } from "../ui/icons";
import {
  type FileLine,
  type FileLocation,
  fileLines,
  fileProblem,
  isMarkdown,
  type LinePick,
  picked,
  pickProblem,
  pickRange,
  tapFileLine,
  truncationNote,
} from "./file";

const WRAP_KEY = "kit.files.wrap";

export interface FileViewProps {
  api: Api;
  session: SessionState;
  location: FileLocation;
  fail(error: unknown): boolean;
  refreshComments(): Promise<void>;
  onClose(): void;
}

/** A file's comments, by whether they sit among its lines or above them. */
export function placeComments(
  comments: readonly Comment[],
  lineCount: number,
): { after: Map<number, Comment[]>; above: Comment[] } {
  const after = new Map<number, Comment[]>();
  const above: Comment[] = [];
  for (const comment of comments) {
    if (comment.stale || comment.endLine > lineCount || comment.endLine < 1) above.push(comment);
    else after.set(comment.endLine, [...(after.get(comment.endLine) ?? []), comment]);
  }
  return { after, above };
}

export function FileView(props: FileViewProps) {
  const id = () => props.session.info.id;
  const path = () => props.location.path;
  const name = () => path().slice(path().lastIndexOf("/") + 1);
  const dir = () => path().slice(0, Math.max(0, path().lastIndexOf("/")));
  const [read, setRead] = createSignal<WorkspaceFileRead>();
  const [problem, setProblem] = createSignal("");
  const [loading, setLoading] = createSignal(false);
  const [mode, setMode] = createSignal<"preview" | "source">("source");
  const [wrap, setWrap] = createSignal(localStorage.getItem(WRAP_KEY) === "1");
  const [copied, setCopied] = createSignal(false);
  const [pick, setPick] = createSignal<LinePick>();
  const [replacing, setReplacing] = createSignal<Comment>();
  const [editing, setEditing] = createSignal<number>();
  const [moveFailed, setMoveFailed] = createSignal<number>();
  let scroller: HTMLDivElement | undefined;
  let menu: HTMLDivElement | undefined;
  let controller: AbortController | undefined;
  // The lines asked for come into view once per request.
  let revealed: string | undefined;
  onCleanup(() => controller?.abort());

  const load = async () => {
    const workspaceId = props.session.workspace?.workspaceId;
    if (!workspaceId) {
      setProblem("This session has no folder.");
      return;
    }
    controller?.abort();
    const current = new AbortController();
    controller = current;
    const asked = path();
    setLoading(true);
    try {
      const result = await props.api.readWorkspaceFile(
        id(),
        { workspaceId, path: asked, highlight: true },
        current.signal,
      );
      if (controller !== current || asked !== path()) return;
      batch(() => {
        setRead(result);
        setProblem("");
      });
      // A file that changed since a comment was made may have left it on other lines.
      const moved = props.session.comments.some(
        (comment) =>
          !comment.stale &&
          comment.anchor.workspaceFile?.path === result.path &&
          comment.anchor.workspaceFile.fileRevision !== result.revision,
      );
      if (moved) void props.refreshComments().catch(() => {});
    } catch (failure) {
      if (isAbort(failure) || controller !== current) return;
      if (props.fail(failure)) return;
      setRead(undefined);
      setProblem(
        fileProblem(failure) ?? describe({ fail: props.fail }, failure, "The file couldn’t be read.") ?? "",
      );
    } finally {
      if (controller === current) setLoading(false);
    }
  };

  // A new file starts over: its own mode, no picked lines, and a fresh read.
  createEffect(
    on(path, () => {
      batch(() => {
        setRead(undefined);
        setProblem("");
        setPick(undefined);
        setReplacing(undefined);
        setEditing(undefined);
      });
      void load();
    }),
  );
  // Markdown opens as a preview unless lines were asked for, which only the source has.
  createEffect(() => {
    setMode(isMarkdown(path()) && !props.location.line ? "preview" : "source");
  });
  // A turn that ends may have changed the file; so may time away from the page.
  createEffect(
    on(
      () => props.session.activeTurnId,
      (turn, previous) => {
        if (previous && !turn) void load();
      },
      { defer: true },
    ),
  );
  const visible = () => {
    if (document.visibilityState === "visible") void load();
  };
  document.addEventListener("visibilitychange", visible);
  onCleanup(() => document.removeEventListener("visibilitychange", visible));

  const lines = createMemo<FileLine[]>(() => {
    const value = read();
    return value ? fileLines(value) : [];
  });
  const comments = createMemo(() =>
    props.session.comments.filter((comment) => comment.anchor.workspaceFile?.path === path()),
  );
  const placed = createMemo(() => placeComments(comments(), lines().length));
  const marked = (line: number) => {
    const start = props.location.line;
    if (!start) return false;
    return line >= start && line <= (props.location.end ?? start);
  };

  // Bring the lines asked for into view once they're drawn.
  createEffect(() => {
    const line = props.location.line;
    const key = `${path()}#${line ?? ""}-${props.location.end ?? ""}`;
    if (!read() || mode() !== "source" || revealed === key) return;
    revealed = key;
    if (!line) {
      scroller?.scrollTo({ top: 0 });
      return;
    }
    requestAnimationFrame(() => {
      scroller?.querySelector(`[data-line="${line}"]`)?.scrollIntoView({ block: "center" });
    });
  });

  const desk: CommentDesk = {
    api: props.api,
    sessionId: id(),
    findComment: (commentId) => props.session.comments.find((comment) => comment.id === commentId),
    clearSelection: () => setPick(undefined),
    replacing,
    setReplacing,
    editing,
    setEditing(value) {
      if (value !== undefined) setPick(undefined);
      setEditing(value);
    },
    moveFailed,
    reportMoveFailure: setMoveFailed,
    refreshComments: () => props.refreshComments(),
    fail: (error) => props.fail(error),
  };

  const tap = (line: number) => {
    setEditing(undefined);
    setPick(tapFileLine(pick(), line));
  };

  const toggleWrap = () => {
    const next = !wrap();
    setWrap(next);
    localStorage.setItem(WRAP_KEY, next ? "1" : "0");
    menu?.hidePopover();
  };
  const copyPath = async () => {
    const done = await copyText(path());
    setCopied(done);
    if (done) setTimeout(() => setCopied(false), 1500);
    menu?.hidePopover();
  };

  const digits = () => String(Math.max(lines().length, 1)).length;

  return (
    <>
      <header class="bar">
        <button type="button" class="icon-button" aria-label="Back" onClick={props.onClose}>
          <Icon name="back" />
        </button>
        <div class="bar-title">
          <h1 class="mono file-title">{name()}</h1>
          <p class="mono muted">{dir() || "."}</p>
        </div>
        <Show when={isMarkdown(path())}>
          <fieldset class="segmented file-mode">
            <legend class="visually-hidden">Show</legend>
            <label>
              <input
                type="radio"
                name="file-mode"
                value="preview"
                checked={mode() === "preview"}
                onChange={() => setMode("preview")}
              />
              Preview
            </label>
            <label>
              <input
                type="radio"
                name="file-mode"
                value="source"
                checked={mode() === "source"}
                onChange={() => setMode("source")}
              />
              Source
            </label>
          </fieldset>
        </Show>
        <button type="button" class="icon-button" popovertarget="file-menu" aria-label="File options">
          <Icon name="more" />
        </button>
        <div id="file-menu" popover class="menu sheet-menu" ref={menu}>
          <button
            type="button"
            class="menu-action"
            role="menuitemcheckbox"
            aria-checked={wrap()}
            onClick={toggleWrap}
          >
            <span class="target-check">
              <Show when={wrap()}>
                <Icon name="check" size={16} />
              </Show>
            </span>
            <span class="grow">Wrap lines</span>
          </button>
          <button type="button" class="menu-action" onClick={copyPath}>
            <Icon name="copy" size={18} />
            <span class="grow">Copy path</span>
          </button>
        </div>
      </header>
      <Show when={copied()}>
        <p class="visually-hidden" role="status">
          Path copied
        </p>
      </Show>
      <div
        class="file-view"
        ref={scroller}
        data-wrap={wrap() || undefined}
        style={{ "--gutter": `${digits() + 2}ch` }}
      >
        <Switch>
          <Match when={problem()}>
            <div class="empty-state">
              <p>{problem()}</p>
              <button type="button" onClick={() => load()}>
                Try again
              </button>
            </div>
          </Match>
          <Match when={!read()}>
            <p class="empty">
              <Spinner /> Reading {name()}…
            </p>
          </Match>
          <Match when={mode() === "preview" && read()}>
            {(value) => (
              <div class="file-preview">
                <Markdown text={value().content} />
                <Show when={truncationNote(value())}>
                  {(note) => <p class="file-note muted small">{note()}</p>}
                </Show>
              </div>
            )}
          </Match>
          <Match when={read()}>
            {(value) => (
              <>
                <Show when={placed().above.length > 0}>
                  <div class="file-notes">
                    <For each={placed().above}>
                      {(comment) => <Note desk={desk} comment={comment} canMove={lines().length > 0} />}
                    </For>
                  </div>
                </Show>
                <Show when={replacing()}>
                  <p class="file-note muted small" role="status">
                    Tap the line numbers this comment is about.
                  </p>
                </Show>
                <Show when={lines().length > 0} fallback={<p class="empty">This file is empty.</p>}>
                  <div class="file-lines">
                    <For each={lines()}>
                      {(line) => (
                        <>
                          <div
                            class="file-row"
                            data-line={line.number}
                            data-marked={marked(line.number) || undefined}
                            data-selected={picked(pick(), line.number) || undefined}
                          >
                            <button
                              type="button"
                              class="file-num"
                              aria-pressed={picked(pick(), line.number)}
                              aria-label={`Line ${line.number}`}
                              onClick={() => tap(line.number)}
                            >
                              {line.number}
                            </button>
                            <span class="file-code">
                              <Show when={line.runs} fallback={line.text}>
                                {(runs) => (
                                  <For each={runs()}>
                                    {(run) =>
                                      run.tone ? <span class={`hl-${run.tone}`}>{run.text}</span> : run.text
                                    }
                                  </For>
                                )}
                              </Show>
                            </span>
                          </div>
                          <For each={placed().after.get(line.number)}>
                            {(comment) => (
                              <div class="file-inset">
                                <Note desk={desk} comment={comment} canMove />
                              </div>
                            )}
                          </For>
                          <Show when={pick() && pickRange(pick() as LinePick).end === line.number && pick()}>
                            {(value) => (
                              <div class="file-inset">
                                <CommentEditor
                                  desk={desk}
                                  start={pickRange(value()).start}
                                  end={pickRange(value()).end}
                                  problem={pickProblem(value(), lines().length)}
                                  anchor={() => {
                                    const workspaceId = props.session.workspace?.workspaceId;
                                    if (!workspaceId) return undefined;
                                    const range = pickRange(value());
                                    return {
                                      kind: "workspace_file",
                                      workspaceFile: {
                                        workspaceId,
                                        path: path(),
                                        fileRevision: read()?.revision ?? "",
                                        startLine: range.start,
                                        endLine: range.end,
                                      },
                                    };
                                  }}
                                  onMovedOn={() => void load()}
                                />
                              </div>
                            )}
                          </Show>
                        </>
                      )}
                    </For>
                  </div>
                </Show>
                <Show when={truncationNote(value())}>
                  {(note) => <p class="file-note muted small">{note()}</p>}
                </Show>
                <Show when={loading()}>
                  <span class="visually-hidden" role="status">
                    Reading the file again
                  </span>
                </Show>
              </>
            )}
          </Match>
        </Switch>
      </div>
    </>
  );
}
