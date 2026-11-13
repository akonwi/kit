// The find bar's side of the page: the texts a file view shows, read from
// what it drew, and the matches drawn back over them with the CSS Custom
// Highlight API, so the browser paints only what's on screen and the lines
// themselves never redraw.
import { type Accessor, createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js";
import { FileFind, type FindCorpus, type FindMatch, type FindPosition, pageFindAction } from "./fileFind";

/** A text the view shows, the text nodes that draw it, and the element it sits in. */
export interface DomPiece {
  text: string;
  nodes: Text[];
  element: Element;
}

/** Elements whose text a rendered Markdown file shows as one piece. */
const BLOCKS = "p, h1, h2, h3, h4, h5, h6, li, td, th, pre, dt, dd";
/** What a rendered Markdown file draws rather than shows as text, and fence chrome. */
const UNSEARCHED = "figure.diagram, figure.figure, figcaption, button, svg, input, .callout-head";

/** The pieces root shows: a source's lines, or a rendered Markdown file's texts. */
export function collectPieces(root: Element, corpus: FindCorpus): DomPiece[] {
  if (corpus === "source")
    return Array.from(root.querySelectorAll(".file-code"), (element) => {
      const nodes = textNodes(element);
      return { text: nodes.map((node) => node.data).join(""), nodes, element };
    });
  if (corpus !== "rendered") return [];
  const pieces: DomPiece[] = [];
  let last: DomPiece | undefined;
  for (const node of textNodes(root.querySelector(".markdown") ?? root)) {
    const parent = node.parentElement;
    if (!parent || unsearched(parent)) continue;
    const block = parent.closest(BLOCKS) ?? parent;
    if (last && last.element === block) {
      last.text += node.data;
      last.nodes.push(node);
    } else {
      last = { text: node.data, nodes: [node], element: block };
      pieces.push(last);
    }
  }
  return pieces;
}

/** Whether element sits in something unsearched: drawings, controls, or a callout's kind and title, which every client treats as chrome. */
function unsearched(element: Element): boolean {
  return element.closest(UNSEARCHED) !== null;
}

function textNodes(root: Element): Text[] {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const nodes: Text[] = [];
  for (let node = walker.nextNode(); node; node = walker.nextNode())
    if ((node as Text).data) nodes.push(node as Text);
  return nodes;
}

/** A match as a DOM range, or undefined when the page no longer has its text. */
export function matchRange(pieces: readonly DomPiece[], match: FindMatch): Range | undefined {
  const piece = pieces[match.piece];
  if (!piece || match.end > piece.text.length) return undefined;
  const range = document.createRange();
  let base = 0;
  let started = false;
  for (const node of piece.nodes) {
    const end = base + node.data.length;
    if (!started && match.start < end) {
      range.setStart(node, match.start - base);
      started = true;
    }
    if (started && match.end <= end) {
      range.setEnd(node, match.end - base);
      return range;
    }
    base = end;
  }
  return undefined;
}

/** Where in pieces a DOM boundary falls, or undefined when it's outside them. */
export function positionOf(
  pieces: readonly DomPiece[],
  node: Node,
  offset: number,
): FindPosition | undefined {
  for (const [index, piece] of pieces.entries()) {
    if (!piece.element.contains(node)) continue;
    let base = 0;
    for (const text of piece.nodes) {
      if (text === node) return { piece: index, offset: base + offset };
      base += text.data.length;
    }
    return { piece: index, offset: 0 };
  }
  return undefined;
}

/** The first piece at least partly below the top of scroller, at its start. */
export function topPosition(pieces: readonly DomPiece[], scroller: Element): FindPosition {
  const top = scroller.getBoundingClientRect().top;
  let low = 0;
  let high = pieces.length;
  while (low < high) {
    const middle = (low + high) >> 1;
    const piece = pieces[middle];
    if (piece && piece.element.getBoundingClientRect().bottom <= top) low = middle + 1;
    else high = middle;
  }
  return { piece: low === pieces.length ? 0 : low, offset: 0 };
}

/**
 * Scrolls range into view unless it shows whole, putting it in the middle
 * of scroller. `inset` is how much of scroller's start a sticky gutter covers.
 */
export function reveal(range: Range, scroller: Element, inset = 0): void {
  const box = range.getBoundingClientRect();
  const view = scroller.getBoundingClientRect();
  let top = 0;
  let left = 0;
  if (box.top < view.top || box.bottom > view.bottom)
    top = (box.top + box.bottom) / 2 - (view.top + view.bottom) / 2;
  if (box.left < view.left + inset || box.right > view.right)
    left = (box.left + box.right) / 2 - (view.left + inset + view.right) / 2;
  if (top || left) scroller.scrollBy({ top, left });
}

/** Whether this browser draws find's highlights. Without them, the count and scrolling still work. */
const highlights = typeof CSS !== "undefined" && "highlights" in CSS && typeof Highlight !== "undefined";

/** Draws every match in one tint and the current one solid. */
export class FindPainter {
  private drawn: readonly FindMatch[] | undefined;
  private ranges: Range[] = [];

  paint(pieces: readonly DomPiece[], matches: readonly FindMatch[], current: FindMatch | undefined): void {
    if (!highlights) return;
    if (this.drawn !== matches) {
      this.drawn = matches;
      this.ranges = [];
      for (const match of matches) {
        const range = matchRange(pieces, match);
        if (range) this.ranges.push(range);
      }
      CSS.highlights.set("kit-find", new Highlight(...this.ranges));
    }
    const range = current && matchRange(pieces, current);
    if (!range) {
      CSS.highlights.delete("kit-find-current");
      return;
    }
    const solid = new Highlight(range);
    solid.priority = 1;
    CSS.highlights.set("kit-find-current", solid);
  }

  /** Forgets the ranges drawn, so the next paint draws them again. */
  invalidate(): void {
    this.drawn = undefined;
  }

  clear(): void {
    this.drawn = undefined;
    this.ranges = [];
    if (!highlights) return;
    CSS.highlights.delete("kit-find");
    CSS.highlights.delete("kit-find-current");
  }
}

/** Whether ⌘ rather than Ctrl leads shortcuts here. */
const APPLE =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent);

/** A file view's find bar, wired to the page. */
export interface FileFindView {
  find: FileFind;
  /** Changes whenever find does. */
  version: Accessor<number>;
  isOpen: Accessor<boolean>;
  toggle(): void;
  /** Takes the bar's field, which opening focuses. */
  setField(field: HTMLInputElement): void;
}

/**
 * Find for a file view: reads the texts scroller shows, searches them as
 * the query or the page changes, draws the matches, shows the current one,
 * and answers ⌘F, ⌘G, ⇧⌘G, and ⌘E anywhere on the page.
 */
export function createFileFind(options: {
  scroller: () => HTMLElement | undefined;
  corpus: Accessor<FindCorpus>;
  /** The lines a cut-short file arrived with, or 0. */
  searchedLines: Accessor<number>;
  path: Accessor<string>;
}): FileFindView {
  const find = new FileFind();
  const [version, setVersion] = createSignal(0);
  find.changed = () => setVersion((value) => value + 1);
  const state = <T>(value: () => T) => {
    version();
    return value();
  };
  // Memos, so their effects run only when a request is made.
  const isOpen = createMemo(() => state(() => find.isOpen));
  const focusRequest = createMemo(() => state(() => find.focusRequest));
  const revealRequest = createMemo(() => state(() => find.revealRequest));
  const dismissRequest = createMemo(() => state(() => find.dismissRequest));
  const painter = new FindPainter();
  let field: HTMLInputElement | undefined;
  let pieces: DomPiece[] = [];
  // Whether the page changed since pieces were read from it.
  let piecesStale = true;
  let markedBlock: Element | undefined;
  // The current match last drawn, which closing leaves selected.
  let lastCurrent: FindMatch | undefined;
  let pending: ReturnType<typeof setTimeout> | undefined;

  const syncPieces = () => {
    if (!piecesStale) return;
    piecesStale = false;
    const corpus = options.corpus();
    const scroller = options.scroller();
    pieces = scroller ? collectPieces(scroller, corpus) : [];
    painter.invalidate();
    find.setPieces(
      pieces.map((piece) => piece.text),
      corpus,
    );
  };
  const paint = () => {
    const match = find.isOpen ? find.currentMatch : undefined;
    if (find.isOpen) {
      lastCurrent = match;
      painter.paint(pieces, find.matches, match);
    } else painter.clear();
    // A rendered file marks the margin of the block the current match is in.
    const block = match && find.corpus === "rendered" ? topBlock(pieces[match.piece]?.element) : undefined;
    if (block === markedBlock) return;
    markedBlock?.removeAttribute("data-find-current");
    block?.setAttribute("data-find-current", "");
    markedBlock = block;
  };
  // Searches once a task at most, after the page has drawn what changed.
  const schedule = () => {
    if (pending) return;
    pending = setTimeout(() => {
      pending = undefined;
      if (!find.isOpen) return;
      syncPieces();
      if (find.stale) find.refresh();
      else paint();
    });
  };
  const pageChanged = () => {
    piecesStale = true;
    if (find.isOpen) schedule();
  };

  find.visibleAnchor = () => {
    const scroller = options.scroller();
    return scroller ? topPosition(pieces, scroller) : { piece: 0, offset: 0 };
  };
  find.selectedText = () => {
    const scroller = options.scroller();
    const selection = getSelection();
    if (!scroller || !selection || selection.isCollapsed || selection.rangeCount === 0) return undefined;
    const range = selection.getRangeAt(0);
    if (!scroller.contains(range.startContainer)) return undefined;
    syncPieces();
    const start = positionOf(pieces, range.startContainer, range.startOffset) ?? find.visibleAnchor();
    return { text: selection.toString(), start };
  };

  createEffect(() => {
    version();
    if (find.isOpen && find.stale) schedule();
    paint();
  });
  createEffect(
    on(
      revealRequest,
      () => {
        const scroller = options.scroller();
        const match = find.currentMatch;
        const range = match && matchRange(pieces, match);
        if (!range || !scroller) return;
        // A source's line numbers stay at its start as it scrolls sideways.
        const gutter = find.corpus === "source" ? scroller.querySelector(".file-num") : null;
        reveal(range, scroller, gutter?.getBoundingClientRect().width ?? 0);
      },
      { defer: true },
    ),
  );
  // Closing gives the keyboard back to the file, with the last match
  // selected where a pointer can use it.
  createEffect(
    on(
      dismissRequest,
      () => {
        options.scroller()?.focus({ preventScroll: true });
        const range = lastCurrent && matchRange(pieces, lastCurrent);
        const selection = getSelection();
        if (range && selection && matchMedia("(pointer: fine)").matches) {
          selection.removeAllRanges();
          selection.addRange(range);
        }
      },
      { defer: true },
    ),
  );
  // Opening, or asking again while open, puts the keyboard in the field with its text selected.
  createEffect(
    on(
      focusRequest,
      () => {
        field?.focus();
        field?.select();
      },
      { defer: true },
    ),
  );
  // Another mode or another read reads the page again; so does anything it redraws.
  createEffect(on(options.corpus, pageChanged));
  createEffect(() => {
    const searched = options.searchedLines();
    if (searched === find.searchedLines) return;
    find.searchedLines = searched;
    find.changed();
  });
  // Another file starts with the bar closed and empty.
  createEffect(
    on(
      options.path,
      () => {
        find.close();
        find.setQuery("");
      },
      { defer: true },
    ),
  );
  onMount(() => {
    const scroller = options.scroller();
    if (!scroller) return;
    const observer = new MutationObserver(pageChanged);
    observer.observe(scroller, { childList: true, subtree: true, characterData: true });
    onCleanup(() => observer.disconnect());
  });

  const key = (event: KeyboardEvent) => {
    if (event.defaultPrevented || event.isComposing || document.querySelector("dialog[open]")) return;
    const inField = event.target instanceof Element && event.target.closest(".file-find") !== null;
    const action = pageFindAction(event, APPLE, inField);
    if (!action || (action === "useSelection" && !find.selectedText())) return;
    event.preventDefault();
    if (action === "open") find.open();
    else if (action === "next") find.next();
    else if (action === "previous") find.previous();
    else find.useSelection();
  };
  document.addEventListener("keydown", key);
  onCleanup(() => {
    document.removeEventListener("keydown", key);
    clearTimeout(pending);
    painter.clear();
  });

  return {
    find,
    version,
    isOpen,
    toggle: () => (find.isOpen ? find.close() : find.open()),
    setField: (value) => {
      field = value;
    },
  };
}

/** The block of a rendered Markdown file that element is in. */
function topBlock(element: Element | undefined): Element | undefined {
  let block = element;
  while (block?.parentElement && !block.parentElement.classList.contains("markdown"))
    block = block.parentElement;
  return block?.parentElement ? block : undefined;
}
