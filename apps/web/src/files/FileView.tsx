// A file in the session's folder, read only, from one download of it (ADR
// 0074): its lines colored in the browser, the lines asked for marked and in
// view, and comments on lines as on Changes. Only the lines near the view
// are drawn, so a long file costs what a short one does. Markdown opens as
// a preview, with its source one tap away.
import {
  batch,
  createEffect,
  createMemo,
  createSignal,
  For,
  Match,
  on,
  onCleanup,
  onMount,
  type Setter,
  Show,
  Switch,
} from "solid-js";
import { type Api, isAbort } from "../api/client";
import type { Comment } from "../changes/comments";
import { type CommentDesk, CommentEditor, describe, Note } from "../changes/Note";
import { Markdown } from "../markdown/Markdown";
import type { SessionState } from "../session/projection";
import { copyText } from "../ui/copy";
import { Icon, Spinner } from "../ui/icons";
import { colorFile, type FileColors } from "./colors";
import { type FileCopy, loadFile } from "./download";
import { FindBar } from "./FindBar";
import {
  BLOCK_LINES,
  blockLines,
  blockOf,
  type FileLocation,
  fileLines,
  fileProblem,
  isMarkdown,
  type LinePick,
  NOT_TEXT,
  picked,
  pickProblem,
  pickRange,
  tapFileLine,
} from "./file";
import { createFileFind } from "./findView";

/** What the view says under a file it shows only the start of. */
const LIMITED = "Showing the start of the file. The rest is too large to show.";

/** The most Markdown, in UTF-16 code units, the view renders as a preview; past it, Markdown shows as source. */
const MAX_PREVIEW = 1 << 20;

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
  const workspaceId = () => props.session.workspace?.workspaceId;
  const name = () => path().slice(path().lastIndexOf("/") + 1);
  const dir = () => path().slice(0, Math.max(0, path().lastIndexOf("/")));
  const [copy, setCopy] = createSignal<FileCopy>();
  const [colors, setColors] = createSignal<FileColors>();
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

  /** Downloads the file, or with `revalidate`, checks the copy shown and downloads it only if it changed. */
  const load = async (revalidate = false) => {
    const folder = workspaceId();
    if (!folder) {
      setProblem("This session has no folder.");
      return;
    }
    controller?.abort();
    const current = new AbortController();
    controller = current;
    const asked = path();
    setLoading(true);
    try {
      const result = await loadFile(
        props.api,
        id(),
        { workspaceId: folder, path: asked },
        { revision: revalidate && !problem() ? copy()?.revision : undefined, signal: current.signal },
      );
      if (controller !== current || asked !== path() || folder !== workspaceId()) return;
      if (result.kind === "unchanged") return;
      if (result.kind === "binary") {
        batch(() => {
          setCopy(undefined);
          setProblem(NOT_TEXT);
        });
        return;
      }
      batch(() => {
        setCopy(result.copy);
        setProblem("");
      });
      // A file that changed since a comment was made may have left it on other lines.
      const revision = result.copy.revision;
      const moved =
        revision !== undefined &&
        props.session.comments.some(
          (comment) =>
            !comment.stale &&
            comment.anchor.workspaceFile?.path === asked &&
            comment.anchor.workspaceFile.fileRevision !== revision,
        );
      if (moved) void props.refreshComments().catch(() => {});
    } catch (failure) {
      if (isAbort(failure) || controller !== current) return;
      if (props.fail(failure)) return;
      setCopy(undefined);
      setProblem(
        fileProblem(failure) ?? describe({ fail: props.fail }, failure, "The file couldn’t be read.") ?? "",
      );
    } finally {
      if (controller === current) setLoading(false);
    }
  };

  // A new file, or the same path in another folder, starts over: its own
  // mode, no picked lines, and a fresh download.
  createEffect(
    on([path, workspaceId], () => {
      batch(() => {
        setCopy(undefined);
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
  /** Whether the copy is small enough to render as a preview. */
  const previewable = () => (copy()?.text.length ?? 0) <= MAX_PREVIEW;
  // Markdown too large to preview opens as its source.
  createEffect(
    on(
      () => copy() !== undefined && !previewable(),
      (large) => {
        if (large) setMode("source");
      },
    ),
  );
  // A turn that ends may have changed the file; so may time away from the page.
  createEffect(
    on(
      () => props.session.activeTurnId,
      (turn, previous) => {
        if (previous && !turn) void load(true);
      },
      { defer: true },
    ),
  );
  const visible = () => {
    if (document.visibilityState === "visible") void load(true);
  };
  document.addEventListener("visibilitychange", visible);
  onCleanup(() => document.removeEventListener("visibilitychange", visible));

  const lines = createMemo<string[]>(() => {
    const value = copy();
    return value ? fileLines(value.text) : [];
  });
  // Colors come after the text, which shows plain until they do.
  createEffect(
    on(lines, (current) => {
      const value = copy();
      setColors(undefined);
      if (!value || current.length === 0) return;
      let live = true;
      let made: FileColors | undefined;
      void colorFile(path(), value.text, current.length).then((result) => {
        made = result;
        if (live) setColors(result);
        else result?.dispose();
      });
      onCleanup(() => {
        live = false;
        made?.dispose();
      });
    }),
  );
  const runsOf = (line: number, text: string) => colors()?.runs(line - 1, text);

  const comments = createMemo(() =>
    props.session.comments.filter((comment) => comment.anchor.workspaceFile?.path === path()),
  );
  const placed = createMemo(() => placeComments(comments(), lines().length));
  const marked = (line: number) => {
    const start = props.location.line;
    if (!start) return false;
    return line >= start && line <= (props.location.end ?? start);
  };
  /** Whether the copy shown takes comments: it has a revision to anchor them to. */
  const annotatable = () => copy()?.revision !== undefined;
  // Lines picked in a copy that can't take comments have nowhere to go.
  createEffect(
    on(copy, (value) => {
      if (value && !annotatable()) setPick(undefined);
    }),
  );

  // Lines are drawn a block at a time, while the block is near the view or
  // holds something the reader is working on; otherwise a block keeps the
  // height it had, or the height its lines would have, so the view scrolls
  // as if every line were there.
  const blocks = createMemo(() =>
    Array.from({ length: Math.ceil(lines().length / BLOCK_LINES) }, (_, index) => index),
  );
  const nearSetters = new Map<Element, Setter<boolean>>();
  const blockSetters = new Map<number, Setter<boolean>>();
  const heights = new Map<number, number>();
  // Changes when heights are forgotten, so blocks out of view take their estimate.
  const [measures, setMeasures] = createSignal(0);
  // Wrapped lines are as tall as the lines measured so far; others, as tall as the text's line height.
  const [lineHeight, setLineHeight] = createSignal<number>();
  let linesElement: HTMLDivElement | undefined;
  const estimate = () => {
    const measured = wrap() ? lineHeight() : undefined;
    if (measured) return measured;
    const css = linesElement ? Number.parseFloat(getComputedStyle(linesElement).lineHeight) : Number.NaN;
    return Number.isFinite(css) && css > 0 ? css : 20;
  };
  let nearObserver: IntersectionObserver | undefined;
  const sizeObserver = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const block = Number((entry.target as HTMLElement).dataset.block);
      if (!(entry.target as HTMLElement).dataset.drawn) continue;
      const height = entry.borderBoxSize[0]?.blockSize ?? entry.contentRect.height;
      heights.set(block, height);
      if (lineHeight() === undefined) {
        const count = blockLines(block, lines().length).length;
        if (count > 0 && height > 0) setLineHeight(height / count);
      }
    }
  });
  onCleanup(() => {
    nearObserver?.disconnect();
    sizeObserver.disconnect();
    widthObserver.disconnect();
  });
  const forgetHeights = () => {
    heights.clear();
    batch(() => {
      setLineHeight(undefined);
      setMeasures((value) => value + 1);
    });
  };
  // Another file, its lines changed, or lines that wrap differently measure again.
  createEffect(on([lines, wrap], forgetHeights));
  // So do wrapped lines in a view of another width.
  let width = 0;
  const widthObserver = new ResizeObserver((entries) => {
    const next = Math.round(entries[0]?.contentRect.width ?? 0);
    if (next === width) return;
    const measured = width > 0;
    width = next;
    if (measured && wrap()) forgetHeights();
  });
  onMount(() => {
    if (scroller) widthObserver.observe(scroller);
  });
  const observeNear = (element: Element, setNear: Setter<boolean>) => {
    if (!nearObserver && scroller) {
      nearObserver = new IntersectionObserver(
        (entries) => {
          for (const entry of entries) nearSetters.get(entry.target)?.(entry.isIntersecting);
        },
        // A screen's worth of lines above and below the view is drawn ahead of it.
        { root: scroller, rootMargin: "100% 0px" },
      );
    }
    nearSetters.set(element, setNear);
    nearObserver?.observe(element);
  };
  const pinned = (block: number) => {
    const current = pick();
    if (current && blockOf(pickRange(current).end) === block) return true;
    const edited = editing();
    if (edited === undefined) return false;
    const comment = placed().after;
    for (const [line, list] of comment) {
      if (blockOf(line) === block && list.some((item) => item.id === edited)) return true;
    }
    return false;
  };
  /** Draws the block a line is in, if it isn't drawn, so it can be shown. */
  const drawLine = (line: number) => blockSetters.get(blockOf(line))?.(true);
  /** Brings a line into view, centered. */
  const revealLine = (line: number) => {
    drawLine(line);
    scroller?.querySelector(`[data-line="${line}"]`)?.scrollIntoView({ block: "center" });
  };

  // Bring the lines asked for into view once they're drawn.
  createEffect(() => {
    const line = props.location.line;
    const key = `${path()}#${line ?? ""}-${props.location.end ?? ""}`;
    if (!copy() || mode() !== "source" || revealed === key) return;
    revealed = key;
    if (!line) {
      scroller?.scrollTo({ top: 0 });
      return;
    }
    requestAnimationFrame(() => revealLine(line));
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
  // The longest line sets the width, so scrolling sideways doesn't change as lines are drawn.
  const longest = createMemo(() => lines().reduce((most, line) => Math.max(most, line.length), 0));

  const fileFind = createFileFind({
    scroller: () => scroller,
    corpus: () => (!copy() ? "none" : mode() === "preview" ? "rendered" : "source"),
    lines,
    drawLine,
    searchedLines: () => (copy()?.limited ? lines().length : 0),
    path,
  });

  const row = (line: number) => {
    const text = () => lines()[line - 1] ?? "";
    const runs = () => runsOf(line, text());
    return (
      <>
        <div
          class="file-row"
          data-line={line}
          data-marked={marked(line) || undefined}
          data-selected={picked(pick(), line) || undefined}
        >
          <button
            type="button"
            class="file-num"
            disabled={!annotatable()}
            aria-pressed={picked(pick(), line)}
            aria-label={`Line ${line}`}
            onClick={() => tap(line)}
          >
            {line}
          </button>
          <span class="file-code">
            <Show when={runs()} fallback={text()}>
              {(list) => (
                <For each={list()}>
                  {(run) => (run.tone ? <span class={`hl-${run.tone}`}>{run.text}</span> : run.text)}
                </For>
              )}
            </Show>
          </span>
        </div>
        <For each={placed().after.get(line)}>
          {(comment) => (
            <div class="file-inset">
              <Note desk={desk} comment={comment} canMove={annotatable()} />
            </div>
          )}
        </For>
        <Show when={pick() && pickRange(pick() as LinePick).end === line && pick()}>
          {(value) => (
            <div class="file-inset">
              <CommentEditor
                desk={desk}
                start={pickRange(value()).start}
                end={pickRange(value()).end}
                problem={pickProblem(value(), lines().length)}
                anchor={() => {
                  const workspaceId = props.session.workspace?.workspaceId;
                  const revision = copy()?.revision;
                  if (!workspaceId || !revision) return undefined;
                  const range = pickRange(value());
                  return {
                    kind: "workspace_file",
                    workspaceFile: {
                      workspaceId,
                      path: path(),
                      fileRevision: revision,
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
    );
  };

  const block = (index: number) => {
    const [near, setNear] = createSignal(false);
    const drawn = () => near() || pinned(index);
    let element: HTMLDivElement | undefined;
    blockSetters.set(index, setNear);
    onMount(() => {
      if (!element) return;
      observeNear(element, setNear);
      sizeObserver.observe(element);
    });
    onCleanup(() => {
      if (blockSetters.get(index) === setNear) blockSetters.delete(index);
      if (!element) return;
      nearSetters.delete(element);
      nearObserver?.unobserve(element);
      sizeObserver.unobserve(element);
    });
    const space = () => {
      if (drawn()) return undefined;
      measures();
      const height = heights.get(index) ?? blockLines(index, lines().length).length * estimate();
      return { "block-size": `${height}px` };
    };
    return (
      <div
        class="file-block"
        ref={element}
        data-block={index}
        data-drawn={drawn() || undefined}
        style={space()}
      >
        <Show when={drawn()}>
          <For each={blockLines(index, lines().length)}>{row}</For>
        </Show>
      </div>
    );
  };

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
        <button
          type="button"
          class="icon-button"
          aria-label="Find in file"
          aria-expanded={fileFind.isOpen()}
          onClick={fileFind.toggle}
        >
          <Icon name="search" />
        </button>
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
      <Show when={fileFind.isOpen()}>
        <FindBar find={fileFind.find} version={fileFind.version} ref={fileFind.setField} />
      </Show>
      <div
        class="file-view"
        ref={scroller}
        tabindex="-1"
        data-wrap={wrap() || undefined}
        style={{ "--gutter": `${digits() + 2}ch`, "--longest": `${longest()}ch` }}
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
          <Match when={!copy()}>
            <p class="empty">
              <Spinner /> Reading {name()}…
            </p>
          </Match>
          <Match when={mode() === "preview" && !previewable()}>
            <p class="empty">This file is too large to preview.</p>
          </Match>
          <Match when={mode() === "preview" && copy()}>
            {(value) => (
              <div class="file-preview">
                <Markdown text={value().text} base={dir()} />
                <Show when={value().limited}>
                  <p class="file-note muted small">{LIMITED}</p>
                </Show>
              </div>
            )}
          </Match>
          <Match when={copy()}>
            {(value) => (
              <>
                <Show when={value().changed}>
                  <div class="file-note muted small file-changed" role="status">
                    <span>The file changed while it loaded, so it can’t take comments.</span>
                    <button type="button" onClick={() => load()}>
                      Load again
                    </button>
                  </div>
                </Show>
                <Show when={placed().above.length > 0}>
                  <div class="file-notes">
                    <For each={placed().above}>
                      {(comment) => (
                        <Note desk={desk} comment={comment} canMove={annotatable() && lines().length > 0} />
                      )}
                    </For>
                  </div>
                </Show>
                <Show when={replacing()}>
                  <p class="file-note muted small" role="status">
                    Tap the line numbers this comment is about.
                  </p>
                </Show>
                <Show when={lines().length > 0} fallback={<p class="empty">This file is empty.</p>}>
                  <div class="file-lines" ref={linesElement}>
                    <For each={blocks()}>{block}</For>
                  </div>
                </Show>
                <Show when={value().limited}>
                  <p class="file-note muted small">{LIMITED}</p>
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
