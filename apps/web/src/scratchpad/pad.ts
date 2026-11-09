// The scratchpad as one browser edits it: the draft, the shared copy it was
// based on, an autosave a moment after typing stops, and, when the shared
// copy changes under unsaved edits, a review of the two before either wins.
// It follows the Apple clients' ScratchpadState.
import { ApiError, NetworkError } from "../api/client";
import type { Scratchpad } from "../api/types";

/** How long an edit waits for more before it is saved. */
export const AUTOSAVE_MS = 1500;
/** The most the server keeps, in UTF-8 bytes. */
export const MAX_BYTES = 64 * 1024;
/** Browsers drop a keepalive request whose body passes 64 KiB. */
const KEEPALIVE_BYTES = 60 * 1024;

export type PadStatus =
  | { kind: "loading" }
  | { kind: "saved" }
  | { kind: "unsaved" }
  | { kind: "saving" }
  | { kind: "conflict" }
  | { kind: "failed"; reason: string };

export interface PadServer {
  get(): Promise<Scratchpad>;
  put(content: string, expectedRevision: string, keepalive: boolean): Promise<Scratchpad>;
}

/**
 * Where an unsaved draft waits on this device until the server has it, so a
 * page that closes before its save lands, or whose save fails, keeps it.
 */
export interface DraftStore {
  read(owner: string): { base: string; draft: string } | undefined;
  /** `base` is the revision the draft was written against. */
  write(owner: string, base: string, draft: string): void;
  clear(owner: string): void;
}

const DRAFT_KEY = "kit.scratchpad.";

/** Drafts in the page's localStorage, by the session that owns the scratchpad. */
export function localDrafts(storage: Storage = localStorage): DraftStore {
  return {
    read(owner) {
      try {
        const value = JSON.parse(storage.getItem(DRAFT_KEY + owner) ?? "null") as unknown;
        if (!value || typeof value !== "object") return undefined;
        const { base, draft } = value as { base?: unknown; draft?: unknown };
        return typeof base === "string" && typeof draft === "string" ? { base, draft } : undefined;
      } catch {
        return undefined;
      }
    },
    write(owner, base, draft) {
      try {
        storage.setItem(DRAFT_KEY + owner, JSON.stringify({ base, draft }));
      } catch {
        // A full or blocked store leaves the draft in the page only.
      }
    },
    clear(owner) {
      try {
        storage.removeItem(DRAFT_KEY + owner);
      } catch {
        // Nothing to clear.
      }
    },
  };
}

/** Forgets every kept draft, as this browser stops being paired. */
export function forgetDrafts(storage: Storage = localStorage): void {
  try {
    const keys = Array.from({ length: storage.length }, (_, index) => storage.key(index));
    for (const key of keys) if (key?.startsWith(DRAFT_KEY)) storage.removeItem(key);
  } catch {
    // Nothing to forget.
  }
}

/** Waits `ms`, then runs `run`; returns a function that cancels it. */
export type Schedule = (run: () => void, ms: number) => () => void;

const encoder = new TextEncoder();

/** Whether revision `a` is after `b`; both are canonical decimal strings. */
export function laterRevision(a: string, b: string): boolean {
  return a.length === b.length ? a > b : a.length > b.length;
}

/** Whether `next` is a newer copy of the same scratchpad than `current`. */
export function newerScratchpad(next: Scratchpad, current: Scratchpad | undefined): boolean {
  if (!current) return true;
  if (next.ownerSessionId !== current.ownerSessionId) return false;
  return laterRevision(next.revision, current.revision);
}

/** Why a save failed, in a few words for the subtitle. */
export function saveProblem(error: unknown): string {
  if (error instanceof NetworkError) return "Kit can’t be reached";
  if (error instanceof ApiError) {
    switch (error.code) {
      case "scratchpad_too_large":
      case "limit_exceeded":
        return "Over the 64 KB limit";
      case "scratchpad_invalid_content":
        return "It has a character Kit can’t save";
      case "rate_limited":
        return "Too many saves; try again shortly";
      case "scratchpad_unavailable":
        return "The scratchpad isn’t available right now";
      case "scratchpad_revision_exhausted":
        return "The scratchpad can’t take more changes";
      case "not_found":
        return "This session isn’t on the server anymore";
    }
    return error.message;
  }
  return "It couldn’t be saved";
}

/** The newer shared copy a refused save carries, if it was a conflict. */
function conflictCopy(error: unknown): Scratchpad | undefined {
  if (!(error instanceof ApiError) || error.code !== "scratchpad_revision_conflict") return undefined;
  const copy = error.details?.scratchpad as Scratchpad | undefined;
  return copy && typeof copy.content === "string" && typeof copy.revision === "string" ? copy : undefined;
}

export class Pad {
  draft = "";
  status: PadStatus = { kind: "loading" };
  /** The shared copy the draft was last based on. */
  shared: Scratchpad | undefined;
  reviewing = false;
  /** The shared copy under review. */
  reviewed: Scratchpad | undefined;
  /** Bumped when the draft is replaced rather than typed, so the editor reloads its text. */
  version = 0;
  private cancelTimer: (() => void) | undefined;
  private savingContent: string | undefined;
  /** Another save was asked for while one ran; how it should go. */
  private again: { keepalive: boolean } | undefined;
  private loading = false;
  /** The revision the draft was written against, which a conflict leaves behind. */
  private base: string | undefined;

  constructor(
    private readonly server: PadServer,
    private readonly schedule: Schedule,
    /** Called after every change a view might show. */
    private readonly changed: () => void,
    private readonly drafts?: DraftStore,
  ) {}

  get dirty(): boolean {
    return this.shared ? this.shared.content !== this.draft : this.draft !== "";
  }

  edit(value: string): void {
    this.draft = value;
    this.keepDraft();
    if (!this.shared) {
      this.status = { kind: "unsaved" };
    } else if (this.status.kind !== "conflict" && !this.reviewing) {
      if (value === this.shared.content) {
        this.status = { kind: "saved" };
        this.stopTimer();
      } else {
        this.status = { kind: "unsaved" };
        this.startTimer();
      }
    }
    this.changed();
  }

  /** Takes a shared copy from the server: a load, a save, or another device's or the agent's edit. */
  observe(record: Scratchpad): void {
    const current = this.shared;
    if (current) {
      if (record.ownerSessionId !== current.ownerSessionId) return;
      if (laterRevision(current.revision, record.revision)) return;
      if (record.revision === current.revision && record.content === current.content) return;
    } else if (this.restore(record)) {
      return;
    }
    const wasDirty = this.dirty;
    const ownSave = this.savingContent === record.content;
    this.shared = record;
    if (!wasDirty || this.draft === record.content) {
      if (this.draft !== record.content) this.version += 1;
      this.draft = record.content;
      this.base = record.revision;
      this.status = { kind: "saved" };
      this.stopTimer();
      this.keepDraft();
    } else if (ownSave) {
      this.base = record.revision;
      this.keepDraft();
      this.status = { kind: "unsaved" };
      this.startTimer();
    } else {
      this.stopTimer();
      this.status = { kind: "conflict" };
      if (this.reviewing) this.reviewed = record;
    }
    this.changed();
  }

  async load(): Promise<void> {
    if (this.shared || this.loading) return;
    this.loading = true;
    try {
      this.observe(await this.server.get());
    } catch (error) {
      if (!this.shared) {
        this.status = { kind: "failed", reason: saveProblem(error) };
        this.changed();
      }
    } finally {
      this.loading = false;
    }
  }

  /**
   * Saves the draft now. A conflict saves only over the copy under review.
   * Returns whether the draft is saved.
   */
  async save(options: { reviewedRevision?: string; keepalive?: boolean } = {}): Promise<boolean> {
    const current = this.shared;
    if (!current) {
      await this.load();
      return !this.dirty;
    }
    if (this.savingContent !== undefined) {
      // Saved once this one lands, with the newest text.
      this.again = { keepalive: Boolean(options.keepalive) || Boolean(this.again?.keepalive) };
      return false;
    }
    this.stopTimer();
    if (this.status.kind === "conflict" && options.reviewedRevision === undefined) return false;
    if (!this.dirty) {
      this.status = { kind: "saved" };
      this.changed();
      return true;
    }
    const content = this.draft;
    const revision = options.reviewedRevision ?? current.revision;
    if (encoder.encode(content).byteLength > MAX_BYTES) {
      this.status = { kind: "failed", reason: "Over the 64 KB limit" };
      this.changed();
      return false;
    }
    this.savingContent = content;
    this.status = { kind: "saving" };
    this.changed();
    try {
      const body = JSON.stringify({ content, expectedRevision: revision });
      const saved = await this.server.put(
        content,
        revision,
        Boolean(options.keepalive) && encoder.encode(body).byteLength < KEEPALIVE_BYTES,
      );
      this.observe(saved);
      // The observed copy may have turned the save into a conflict.
      if (this.shared?.revision !== saved.revision || this.inConflict()) return false;
      this.reviewing = false;
      this.reviewed = undefined;
      this.changed();
      return !this.dirty;
    } catch (error) {
      const latest = conflictCopy(error);
      if (latest) {
        this.savingContent = undefined;
        this.observe(latest);
        if (this.dirty) {
          this.status = { kind: "conflict" };
          if (this.reviewing) this.reviewed = latest;
        }
      } else if (!this.inConflict()) {
        this.status = { kind: "failed", reason: saveProblem(error) };
      }
      this.changed();
      return false;
    } finally {
      this.savingContent = undefined;
      const again = this.again;
      this.again = undefined;
      if (again && this.dirty && (this.status as PadStatus).kind === "unsaved") void this.save(again);
    }
  }

  /** Saves an unsaved draft straight away, as the page goes away. */
  flush(): void {
    if (this.status.kind === "unsaved" || (this.status.kind === "failed" && this.shared && this.dirty))
      void this.save({ keepalive: true });
  }

  review(): void {
    if (this.status.kind !== "conflict" || !this.shared) return;
    this.reviewed = this.shared;
    this.reviewing = true;
    this.changed();
  }

  keepEditing(): void {
    this.reviewing = false;
    this.changed();
  }

  /** Drops the draft for the shared copy. */
  useShared(): void {
    if (!this.shared) return;
    this.stopTimer();
    if (this.draft !== this.shared.content) this.version += 1;
    this.draft = this.shared.content;
    this.base = this.shared.revision;
    this.keepDraft();
    this.status = { kind: "saved" };
    this.reviewing = false;
    this.reviewed = undefined;
    this.changed();
  }

  /** Overwrites the copy under review with the draft. */
  async replaceShared(): Promise<boolean> {
    const revision = this.reviewed?.revision;
    if (!this.reviewing || !revision) return false;
    return this.save({ reviewedRevision: revision });
  }

  dispose(): void {
    this.stopTimer();
  }

  /**
   * Takes back a draft this device didn't get saved. Written against the copy
   * the server has, it saves; against an older one, it waits as a conflict.
   */
  private restore(record: Scratchpad): boolean {
    const kept = this.drafts?.read(record.ownerSessionId);
    if (!kept || kept.draft === record.content) return false;
    this.shared = record;
    this.draft = kept.draft;
    this.base = kept.base;
    this.version += 1;
    if (kept.base === record.revision) {
      this.status = { kind: "unsaved" };
      this.startTimer();
    } else {
      this.status = { kind: "conflict" };
    }
    this.changed();
    return true;
  }

  /** Keeps an unsaved draft on this device, and forgets a saved one. */
  private keepDraft(): void {
    const owner = this.shared?.ownerSessionId;
    if (!owner || !this.drafts) return;
    if (this.dirty) this.drafts.write(owner, this.base ?? this.shared?.revision ?? "", this.draft);
    else this.drafts.clear(owner);
  }

  private inConflict(): boolean {
    return this.status.kind === "conflict";
  }

  private startTimer(): void {
    this.stopTimer();
    this.cancelTimer = this.schedule(() => {
      this.cancelTimer = undefined;
      void this.save();
    }, AUTOSAVE_MS);
  }

  private stopTimer(): void {
    this.cancelTimer?.();
    this.cancelTimer = undefined;
  }
}

export interface ReviewLine {
  kind: "same" | "shared" | "draft";
  text: string;
}

/**
 * The shared copy against the draft, line by line: lines only the shared
 * copy has, then lines only the draft has, within each changed run.
 */
export function compareCopies(shared: string, draft: string): ReviewLine[] {
  const a = shared.split("\n");
  const b = draft.split("\n");
  // A longest common subsequence table; a scratchpad is at most 64 KiB.
  const n = a.length;
  const m = b.length;
  if (n * m > 4_000_000) {
    return [
      ...a.map((text) => ({ kind: "shared" as const, text })),
      ...b.map((text) => ({ kind: "draft" as const, text })),
    ];
  }
  const width = m + 1;
  const table = new Uint32Array((n + 1) * width);
  for (let i = n - 1; i >= 0; i -= 1) {
    for (let j = m - 1; j >= 0; j -= 1) {
      table[i * width + j] =
        a[i] === b[j]
          ? (table[(i + 1) * width + j + 1] ?? 0) + 1
          : Math.max(table[(i + 1) * width + j] ?? 0, table[i * width + j + 1] ?? 0);
    }
  }
  const lines: ReviewLine[] = [];
  const removed: string[] = [];
  const added: string[] = [];
  const flush = () => {
    for (const text of removed) lines.push({ kind: "shared", text });
    for (const text of added) lines.push({ kind: "draft", text });
    removed.length = 0;
    added.length = 0;
  };
  let i = 0;
  let j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && a[i] === b[j]) {
      flush();
      lines.push({ kind: "same", text: a[i] ?? "" });
      i += 1;
      j += 1;
    } else if (j >= m || (i < n && (table[(i + 1) * width + j] ?? 0) >= (table[i * width + j + 1] ?? 0))) {
      removed.push(a[i] ?? "");
      i += 1;
    } else {
      added.push(b[j] ?? "");
      j += 1;
    }
  }
  flush();
  return lines;
}
