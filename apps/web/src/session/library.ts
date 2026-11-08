// Attachments already on the server, for the transcript, the composer's
// restored files, and the viewer. Their bytes need the device's token, so
// images load with fetch into object URLs, a few at a time, once per page.
import { type Accessor, createSignal, type Setter } from "solid-js";
import type { Api } from "../api/client";
import type { AttachmentInfo } from "../api/types";

/** Undefined while loading; null when the server doesn't have it or it failed. */
type Slot<T> = [Accessor<T | null | undefined>, Setter<T | null | undefined>];

const MAX_DOWNLOADS = 4;
const MAX_RESOLVE = 512;

export class AttachmentLibrary {
  private readonly infos = new Map<string, Slot<AttachmentInfo>>();
  private readonly images = new Map<string, Slot<string>>();
  private readonly blobs = new Map<string, Promise<Blob>>();
  private readonly urls: string[] = [];
  private unresolved: string[] = [];
  private queue: (() => Promise<void>)[] = [];
  private downloading = 0;
  private readonly controller = new AbortController();
  private disposed = false;

  constructor(
    private readonly api: Api,
    private readonly session: string,
    private readonly createURL: (blob: Blob) => string = (blob) => URL.createObjectURL(blob),
    private readonly revokeURL: (url: string) => void = (url) => URL.revokeObjectURL(url),
  ) {}

  /** The attachment's record: name, type, size, and an image's dimensions. */
  info(id: string): AttachmentInfo | null | undefined {
    let slot = this.infos.get(id);
    if (!slot) {
      slot = createSignal<AttachmentInfo | null>();
      this.infos.set(id, slot);
      this.unresolved.push(id);
      // One request for every attachment a render asks about.
      if (this.unresolved.length === 1) queueMicrotask(() => void this.resolve());
    }
    return slot[0]();
  }

  /** An image as an object URL the page may show. */
  image(id: string): string | null | undefined {
    let slot = this.images.get(id);
    if (!slot) {
      const created = createSignal<string | null>();
      slot = created;
      this.images.set(id, slot);
      this.schedule(async () => {
        try {
          const blob = await this.blob(id);
          if (this.disposed) return;
          const url = this.createURL(blob);
          this.urls.push(url);
          created[1](url);
        } catch {
          if (!this.disposed) created[1](null);
        }
      });
    }
    return slot[0]();
  }

  /** The attachment's bytes, read once. */
  blob(id: string): Promise<Blob> {
    let pending = this.blobs.get(id);
    if (!pending) {
      pending = this.api.readAttachment(this.session, id, this.controller.signal);
      // A failed read may be tried again.
      pending.catch(() => this.blobs.delete(id));
      this.blobs.set(id, pending);
    }
    return pending;
  }

  dispose(): void {
    this.disposed = true;
    this.controller.abort();
    this.queue = [];
    for (const url of this.urls) this.revokeURL(url);
    this.urls.length = 0;
  }

  private async resolve(): Promise<void> {
    const ids = this.unresolved;
    this.unresolved = [];
    for (let start = 0; start < ids.length; start += MAX_RESOLVE) {
      const chunk = ids.slice(start, start + MAX_RESOLVE);
      try {
        const result = await this.api.resolveAttachments(this.session, chunk, this.controller.signal);
        const found = new Map(result.attachments.map((info) => [info.id, info]));
        for (const id of chunk) this.infos.get(id)?.[1](found.get(id) ?? null);
      } catch {
        if (this.controller.signal.aborted) return;
        for (const id of chunk) this.infos.get(id)?.[1](null);
      }
    }
  }

  private schedule(task: () => Promise<void>): void {
    this.queue.push(task);
    this.drain();
  }

  private drain(): void {
    while (this.downloading < MAX_DOWNLOADS && this.queue.length > 0) {
      const task = this.queue.shift();
      if (!task) break;
      this.downloading += 1;
      void task().finally(() => {
        this.downloading -= 1;
        this.drain();
      });
    }
  }
}
