// The file view's find bar, apart from the page: what it searches, its query
// and options, what it found, and which match is current. The view supplies
// the text, says where it shows, and carries out the requests the bar counts
// (focus the field, show the current match, give the keyboard back).
import { defaultFindLimit, type FindOptions, findInText, findLabel } from "./find";

/**
 * A place in the text a find bar searches: a UTF-16 offset into one of its
 * pieces. A file's source has a piece for each line; a rendered Markdown
 * file has one for each text it shows (a paragraph, heading, list item,
 * table cell, or code block).
 */
export interface FindPosition {
  piece: number;
  offset: number;
}

/** A match: a half-open UTF-16 range in one piece. */
export interface FindMatch {
  piece: number;
  start: number;
  end: number;
}

/** What the view shows, which decides what its pieces are. */
export type FindCorpus = "none" | "source" | "rendered";

const before = (a: FindPosition, b: FindPosition) =>
  a.piece < b.piece || (a.piece === b.piece && a.offset < b.offset);

/**
 * The matches of query in pieces, in order, counting at most the limit
 * across them all. No match spans two pieces.
 */
export function searchPieces(
  pieces: readonly string[],
  query: string,
  options: FindOptions = {},
): { matches: FindMatch[]; capped: boolean } {
  const matches: FindMatch[] = [];
  const limit = options.limit && options.limit > 0 ? options.limit : defaultFindLimit;
  for (const [piece, text] of pieces.entries()) {
    const remaining = limit - matches.length;
    // Past the limit, one more match anywhere caps the count.
    if (remaining === 0) {
      if (findInText(text, query, { ...options, limit: 1 }).matches.length > 0)
        return { matches, capped: true };
      continue;
    }
    const found = findInText(text, query, { ...options, limit: remaining });
    for (const range of found.matches) matches.push({ piece, ...range });
    if (found.capped) return { matches, capped: true };
  }
  return { matches, capped: false };
}

/**
 * The index of the first match starting at or after anchor, the first when
 * none does, or undefined when there are none.
 */
export function followingPosition(matches: readonly FindMatch[], anchor: FindPosition): number | undefined {
  if (matches.length === 0) return undefined;
  let low = 0;
  let high = matches.length;
  while (low < high) {
    const middle = (low + high) >> 1;
    const match = matches[middle];
    if (match && before({ piece: match.piece, offset: match.start }, anchor)) low = middle + 1;
    else high = middle;
  }
  return low === matches.length ? 0 : low;
}

/** The text a find bar searches for from a selection: its first line. */
export function selectionQuery(text: string): string {
  return text.split(/\r\n|\r|\n/).find((line) => line !== "") ?? "";
}

export class FileFind {
  isOpen = false;
  query = "";
  matchCase = false;
  wholeWord = false;
  /** The lines a cut-short file arrived with, or 0 when it arrived whole. */
  searchedLines = 0;

  corpus: FindCorpus = "none";
  pieces: readonly string[] = [];
  matches: FindMatch[] = [];
  capped = false;
  current: number | undefined;
  /** Whether what a search runs on changed since the last one. */
  stale = false;

  /** Asks the bar's field to take the keyboard and select its text. */
  focusRequest = 0;
  /** Asks the file's view to show the current match. */
  revealRequest = 0;
  /** Asks the file's view to take the keyboard back, the bar having closed. */
  dismissRequest = 0;

  /** Where a search starts looking when nothing else says: the top of what the view shows. */
  visibleAnchor: () => FindPosition = () => ({ piece: 0, offset: 0 });
  /** The text selected in the file, for Use Selection for Find. */
  selectedText: () => { text: string; start: FindPosition } | undefined = () => undefined;
  /** Called after anything here changes. */
  changed: () => void = () => {};

  /** Where the next search starts instead of the current match or the view. */
  private pendingAnchor: FindPosition | undefined;
  /** Where the current match was when the bar closed. */
  private resumeAnchor: FindPosition | undefined;
  /** Steps to take once the next search finishes, from Find Next or Previous with the bar closed. */
  private pendingStep = 0;

  get currentMatch(): FindMatch | undefined {
    return this.current === undefined ? undefined : this.matches[this.current];
  }

  /** What the bar's count reads; empty with nothing typed, or until a search picks a current match. */
  get count(): string {
    if (!this.query || (this.stale && this.current === undefined)) return "";
    return findLabel(this.current === undefined ? 0 : this.current + 1, this.matches.length, this.capped);
  }

  /** What the bar says under its field about a file that arrived cut short; empty otherwise. */
  get note(): string {
    if (!this.query || this.searchedLines <= 0) return "";
    return `In the first ${this.searchedLines.toLocaleString("en-US")} lines`;
  }

  /** Opens the bar, or with it open, selects its field's text again. */
  open(): void {
    if (!this.isOpen) {
      this.isOpen = true;
      this.stale = true;
    }
    this.focusRequest++;
    this.changed();
  }

  /** Closes the bar. The current match stays selected, and Find Next resumes from it. */
  close(): void {
    if (!this.isOpen) return;
    this.resumeAnchor = this.currentMatch && {
      piece: this.currentMatch.piece,
      offset: this.currentMatch.start,
    };
    this.isOpen = false;
    this.dismissRequest++;
    this.current = undefined;
    this.changed();
  }

  /** Moves to the next match, wrapping. With the bar closed, reopens it and steps from where it was. */
  next(): void {
    this.step(1);
  }

  /** Moves to the previous match, wrapping. */
  previous(): void {
    this.step(-1);
  }

  /**
   * Searches for the first line of the text selected in the file, from
   * where it starts. With the bar closed, Find Next then opens it there.
   */
  useSelection(): void {
    const selection = this.selectedText();
    const query = selection ? selectionQuery(selection.text) : "";
    if (!selection || !query) return;
    this.pendingAnchor = selection.start;
    this.current = undefined;
    if (this.isOpen) this.focusRequest++;
    this.query = query;
    this.searchChanged();
  }

  setQuery(query: string): void {
    if (query === this.query) return;
    this.query = query;
    this.searchChanged();
  }

  setMatchCase(on: boolean): void {
    if (on === this.matchCase) return;
    this.matchCase = on;
    this.searchChanged();
  }

  setWholeWord(on: boolean): void {
    if (on === this.wholeWord) return;
    this.wholeWord = on;
    this.searchChanged();
  }

  /**
   * Sets the text searched. A change of what's shown, as from rendered
   * Markdown to its source, forgets the current match, whose place meant
   * something only in the old pieces.
   */
  setPieces(pieces: readonly string[], corpus: FindCorpus): void {
    if (corpus === this.corpus && samePieces(pieces, this.pieces)) return;
    if (corpus !== this.corpus) {
      this.current = undefined;
      this.pendingAnchor = undefined;
      this.resumeAnchor = undefined;
    }
    this.corpus = corpus;
    this.pieces = pieces;
    this.stale = true;
    this.changed();
  }

  /**
   * Runs the search and picks the current match: the first at or after
   * where the last one was, a used selection, or the top of the view.
   */
  refresh(): void {
    const anchor =
      this.pendingAnchor ??
      (this.currentMatch && { piece: this.currentMatch.piece, offset: this.currentMatch.start }) ??
      this.visibleAnchor();
    const found = searchPieces(this.pieces, this.query, {
      matchCase: this.matchCase,
      wholeWord: this.wholeWord,
    });
    this.pendingAnchor = undefined;
    this.stale = false;
    this.matches = found.matches;
    this.capped = found.capped;
    this.current = followingPosition(found.matches, anchor);
    if (this.current !== undefined && this.pendingStep !== 0) {
      const count = this.matches.length;
      this.current = (this.current + (this.pendingStep % count) + count) % count;
    }
    this.pendingStep = 0;
    if (this.current !== undefined) this.revealRequest++;
    this.changed();
  }

  private step(by: number): void {
    if (!this.query) {
      this.open();
      return;
    }
    if (!this.isOpen) {
      this.isOpen = true;
      this.stale = true;
      this.pendingAnchor = this.pendingAnchor ?? this.resumeAnchor;
      this.pendingStep = by;
      this.changed();
      return;
    }
    if (this.current === undefined || this.matches.length === 0) return;
    const count = this.matches.length;
    this.current = (this.current + by + count) % count;
    this.revealRequest++;
    this.changed();
  }

  /**
   * Clears what was found once the query is gone. Otherwise the current
   * match stays until the next search, which starts from it, so typing more
   * of a word keeps its place.
   */
  private searchChanged(): void {
    if (!this.query) {
      this.matches = [];
      this.capped = false;
      this.current = undefined;
    }
    this.stale = true;
    this.changed();
  }
}

function samePieces(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false;
  for (let index = 0; index < a.length; index++) if (a[index] !== b[index]) return false;
  return true;
}

/** What a key does to a file's find bar. */
export type FindAction = "open" | "next" | "previous" | "useSelection" | "close";

/** The parts of a key press find looks at. */
export interface FindKey {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}

/**
 * What a key pressed anywhere on a file's page does to its find bar: ⌘F
 * opens it, ⌘G and ⇧⌘G step, and ⌘E finds the selection, with Ctrl in
 * place of ⌘ off Apple's systems. ⌘F in the bar's own field is left to the
 * browser, so pressing it again opens the browser's find.
 */
export function pageFindAction(event: FindKey, apple: boolean, inField: boolean): FindAction | undefined {
  const command = apple ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  if (!command || event.altKey) return undefined;
  switch (event.key.toLowerCase()) {
    case "f":
      return event.shiftKey || inField ? undefined : "open";
    case "g":
      return event.shiftKey ? "previous" : "next";
    case "e":
      return event.shiftKey ? undefined : "useSelection";
    default:
      return undefined;
  }
}

/** What a key pressed in the bar's field does: Return steps on, Shift-Return back, Esc closes. */
export function fieldFindAction(event: FindKey): FindAction | undefined {
  if (event.metaKey || event.ctrlKey || event.altKey) return undefined;
  if (event.key === "Enter") return event.shiftKey ? "previous" : "next";
  if (event.key === "Escape" && !event.shiftKey) return "close";
  return undefined;
}
