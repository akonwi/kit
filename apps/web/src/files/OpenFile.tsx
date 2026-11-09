// Open File: one fuzzy search over the session folder's files, as the server
// lists them. Enter or a tap opens the file; the server says when its list
// stops short of the whole folder.
import { createEffect, createMemo, createSignal, For, on, Show } from "solid-js";
import { type Api, isAbort } from "../api/client";
import type { SessionFileIndex } from "../api/types";
import { Icon, Spinner } from "../ui/icons";
import { Modal } from "../ui/Modal";
import { markedParts, searchPaths } from "./search";

/** How many results the list draws. */
const SHOWN = 100;

export function OpenFileSheet(props: {
  api: Api;
  sessionId: string;
  folder: string;
  open: boolean;
  fail(error: unknown): boolean;
  onOpen(path: string): void;
  onClose(): void;
}) {
  const [index, setIndex] = createSignal<SessionFileIndex>();
  const [error, setError] = createSignal("");
  const [query, setQuery] = createSignal("");
  const [active, setActive] = createSignal(0);
  let field: HTMLInputElement | undefined;
  let list: HTMLDivElement | undefined;
  let controller: AbortController | undefined;

  const load = async () => {
    controller?.abort();
    const current = new AbortController();
    controller = current;
    setError("");
    try {
      const result = await props.api.fileIndex(props.sessionId, current.signal);
      if (controller === current) setIndex(result);
    } catch (failure) {
      if (isAbort(failure) || controller !== current || props.fail(failure)) return;
      setError(
        failure instanceof Error && failure.message ? failure.message : "The files couldn’t be listed.",
      );
    }
  };

  // Each opening starts a fresh search over a fresh list; the last list shows meanwhile.
  createEffect(
    on(
      () => props.open,
      (open) => {
        if (!open) {
          controller?.abort();
          return;
        }
        setQuery("");
        setActive(0);
        void load();
        requestAnimationFrame(() => field?.focus());
      },
    ),
  );

  const files = createMemo(
    () =>
      index()
        ?.entries.filter((entry) => !entry.isDir)
        .map((entry) => entry.path) ?? [],
  );
  const results = createMemo(() => searchPaths(query(), files(), SHOWN));
  createEffect(on(query, () => setActive(0), { defer: true }));

  const open = (path: string | undefined) => {
    if (!path) return;
    props.onOpen(path);
  };

  const move = (by: number) => {
    const count = results().length;
    if (count === 0) return;
    const next = (active() + by + count) % count;
    setActive(next);
    list?.querySelector(`#open-file-${next}`)?.scrollIntoView({ block: "nearest" });
  };

  return (
    <Modal
      open={props.open}
      onClose={props.onClose}
      drawer
      class="open-file"
      title="Open File"
      detail={<span class="mono">{props.folder}</span>}
      footer={
        index()?.truncated ? (
          <p class="open-file-note muted small">
            <Icon name="info" size={16} />
            <span>This folder has more files than Kit lists. Search covers the first 4,000.</span>
          </p>
        ) : undefined
      }
    >
      <label class="open-file-search">
        <Icon name="search" size={16} />
        <span class="visually-hidden">Search files</span>
        <input
          ref={field}
          type="search"
          aria-controls="open-file-results"
          autocomplete="off"
          autocapitalize="off"
          spellcheck={false}
          placeholder={
            files().length > 0 ? `Search ${files().length.toLocaleString("en-US")} files` : "Search files"
          }
          value={query()}
          onInput={(event) => setQuery(event.currentTarget.value)}
          onKeyDown={(event) => {
            if (event.key === "ArrowDown") {
              event.preventDefault();
              move(1);
            } else if (event.key === "ArrowUp") {
              event.preventDefault();
              move(-1);
            } else if (event.key === "Enter") {
              event.preventDefault();
              open(results()[active()]?.path);
            }
          }}
        />
      </label>
      <Show
        when={index()}
        fallback={
          <Show
            when={error()}
            fallback={
              <p class="open-file-empty muted">
                <Spinner /> Listing files…
              </p>
            }
          >
            <div class="open-file-empty">
              <p class="error">{error()}</p>
              <button type="button" onClick={() => load()}>
                Try again
              </button>
            </div>
          </Show>
        }
      >
        <div id="open-file-results" class="open-file-results" ref={list}>
          <For each={results()}>
            {(result, position) => {
              const at = () => result.path.lastIndexOf("/");
              const name = () => result.path.slice(at() + 1);
              const dir = () => result.path.slice(0, Math.max(0, at()));
              const nameStart = () => Array.from(result.path.slice(0, at() + 1)).length;
              return (
                <button
                  type="button"
                  id={`open-file-${position()}`}
                  class="open-file-result"
                  data-active={active() === position() || undefined}
                  onPointerMove={() => setActive(position())}
                  onClick={() => open(result.path)}
                >
                  <Icon name="file" size={16} />
                  <span class="grow">
                    <span class="open-file-name mono">
                      <For each={markedParts(name(), result.matches, nameStart())}>
                        {(part) => (part.hit ? <mark>{part.text}</mark> : part.text)}
                      </For>
                    </span>
                    <Show when={dir()}>
                      <span class="open-file-dir mono muted">
                        <For each={markedParts(dir(), result.matches)}>
                          {(part) => (part.hit ? <mark>{part.text}</mark> : part.text)}
                        </For>
                      </span>
                    </Show>
                  </span>
                </button>
              );
            }}
          </For>
        </div>
        <Show when={results().length === 0}>
          <p class="open-file-empty muted">
            {files().length === 0 ? "This folder has no files." : "No files match."}
          </p>
        </Show>
      </Show>
    </Modal>
  );
}
