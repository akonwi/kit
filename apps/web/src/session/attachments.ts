// Files a message carries. The composer checks each one as the server will
// before uploading it, says why one isn't attached, and uploads the rest as
// soon as they're added; Send waits for every upload.
import { type Accessor, batch, createSignal, type Setter } from "solid-js";
import { type Api, ApiError } from "../api/client";
import type { AttachmentInfo } from "../api/types";

// The server's limits (api/contract/attachment.go, droids/image_ingest.go).
export const MAX_ATTACHMENTS = 8;
export const MAX_IMAGE_BYTES = 10 << 20;
export const MAX_TEXT_BYTES = 1 << 20;
export const MAX_TOTAL_BYTES = 20 << 20;
export const MAX_IMAGE_SIDE = 8192;
export const MAX_IMAGE_PIXELS = 12_000_000;

/** Why a file wasn't attached, in the Mac app's words. */
export const REFUSAL = {
  count: "A message can contain up to eight attachments.",
  total: "Attachments must total 20 MB or less.",
  size: "Choose a nonempty image up to 10 MB, or a text file up to 1 MB.",
  dimensions: "Images must be at most 8192 pixels per side and 12 megapixels.",
  type: "Attach a PNG, JPEG, GIF, WebP, or UTF-8 text file up to 1 MB.",
  converted: "The converted image exceeds the 10 MB attachment limit.",
} as const;

export function imageRefusal(model: string, filename: string): string {
  return `${model} doesn’t accept images, so ${filename} wasn’t attached.`;
}

export function imagesBlocked(model: string): string {
  return `${model} doesn’t accept images. Remove them to send.`;
}

/** A file the composer won't attach, and why. */
export class AttachmentRefused extends Error {
  override name = "AttachmentRefused";
}

export type ImageType = "image/png" | "image/jpeg" | "image/gif" | "image/webp";

/** The image format a file's first bytes declare, as the server sniffs it. */
export function sniffImage(head: Uint8Array): ImageType | undefined {
  const at = (offset: number, ...values: number[]) => values.every((value, i) => head[offset + i] === value);
  if (at(0, 0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a)) return "image/png";
  if (at(0, 0xff, 0xd8, 0xff)) return "image/jpeg";
  if (at(0, 0x47, 0x49, 0x46, 0x38)) return "image/gif";
  if (at(0, 0x52, 0x49, 0x46, 0x46) && at(8, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50)) return "image/webp";
  return undefined;
}

/** Whether bytes are text the server takes: UTF-8 with no NUL. */
export function isText(data: Uint8Array): boolean {
  if (data.includes(0)) return false;
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(data);
    return true;
  } catch {
    return false;
  }
}

/** A decoded image's size, and a PNG of it for formats the server doesn't take. */
export interface DecodedImage {
  width: number;
  height: number;
  png(): Promise<Blob | undefined>;
  close(): void;
}

export type Decoder = (file: Blob) => Promise<DecodedImage | undefined>;

/** Decodes with the browser; undefined when it can't. */
export const decodeImage: Decoder = async (file) => {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    return undefined;
  }
  return {
    width: bitmap.width,
    height: bitmap.height,
    async png() {
      const canvas = document.createElement("canvas");
      canvas.width = bitmap.width;
      canvas.height = bitmap.height;
      canvas.getContext("2d")?.drawImage(bitmap, 0, 0);
      return new Promise<Blob | undefined>((resolve) =>
        canvas.toBlob((blob) => resolve(blob ?? undefined), "image/png"),
      );
    },
    close: () => bitmap.close(),
  };
};

export interface Prepared {
  file: Blob;
  filename: string;
  image: boolean;
}

/**
 * Checks a file as the server will: a PNG, JPEG, GIF or WebP image up to
 * 10 MB, 8192 px a side and 12 megapixels, or UTF-8 text up to 1 MB. Another
 * image format the browser can read, such as HEIC, becomes a PNG.
 */
export async function prepare(
  file: Blob,
  filename: string,
  decode: Decoder = decodeImage,
): Promise<Prepared> {
  if (file.size === 0 || file.size > MAX_IMAGE_BYTES) throw new AttachmentRefused(REFUSAL.size);
  const head = new Uint8Array(await file.slice(0, 16).arrayBuffer());
  const type = sniffImage(head);
  const textual = !type && file.size <= MAX_TEXT_BYTES && isText(new Uint8Array(await file.arrayBuffer()));
  if (textual) return { file, filename, image: false };
  // Not text: an image, or something the browser can turn into one.
  if (!type && !file.type.startsWith("image/")) throw new AttachmentRefused(REFUSAL.type);
  const decoded = await decode(file);
  if (!decoded) throw new AttachmentRefused(REFUSAL.type);
  try {
    const { width, height } = decoded;
    if (
      width <= 0 ||
      height <= 0 ||
      width > MAX_IMAGE_SIDE ||
      height > MAX_IMAGE_SIDE ||
      width * height > MAX_IMAGE_PIXELS
    ) {
      throw new AttachmentRefused(REFUSAL.dimensions);
    }
    if (type) return { file, filename, image: true };
    const png = await decoded.png();
    if (!png) throw new AttachmentRefused(REFUSAL.type);
    if (png.size > MAX_IMAGE_BYTES) throw new AttachmentRefused(REFUSAL.converted);
    return { file: png, filename: `${filename.replace(/\.[^./]*$/, "") || "image"}.png`, image: true };
  } finally {
    decoded.close();
  }
}

/** One file in the composer's strip. */
export interface DraftAttachment {
  key: string;
  filename: string;
  size: number;
  /** Known once the file is checked; a file being checked shows as a file. */
  image: boolean;
  /** A local image's thumbnail, as an object URL. */
  preview?: string;
  /** The server's record once uploaded, or for a file restored from the queue. */
  info?: AttachmentInfo;
  /** Why the upload failed; Retry sends it again. */
  error?: string;
  /** Being checked or uploaded. */
  busy: boolean;
}

export interface DraftNotice {
  /** A warning for the model, an error for a limit. */
  tone: "warning" | "error";
  text: string;
}

export interface DraftOptions {
  /** The session model's name when it takes no images. */
  imageRejectingModel(): string | undefined;
  /** Hands a failure to the app, such as a revoked device; true when it did. */
  fail(error: unknown): boolean;
  decode?: Decoder;
  createURL?(blob: Blob): string;
  revokeURL?(url: string): void;
}

let keys = 0;

/**
 * A message being written: its text and attachments. It belongs to the
 * session page, so it outlives the composer while a request takes its place.
 */
export class Draft {
  readonly text: Accessor<string>;
  readonly setText: Setter<string>;
  readonly items: Accessor<DraftAttachment[]>;
  readonly notice: Accessor<DraftNotice | undefined>;
  private readonly setItems: Setter<DraftAttachment[]>;
  private readonly setNotice: Setter<DraftNotice | undefined>;
  private readonly restoring: Accessor<number>;
  private readonly setRestoring: Setter<number>;
  private disposed = false;
  private readonly files = new Map<string, Blob>();
  private readonly uploads = new Map<string, AbortController>();
  /** Checks run one at a time, in the strip's order, so earlier files come first. */
  private checking: Promise<void> = Promise.resolve();
  private readonly createURL: (blob: Blob) => string;
  private readonly revokeURL: (url: string) => void;

  constructor(
    private readonly api: Api,
    private readonly session: string,
    private readonly options: DraftOptions,
    text = "",
  ) {
    [this.text, this.setText] = createSignal(text);
    [this.items, this.setItems] = createSignal<DraftAttachment[]>([]);
    [this.notice, this.setNotice] = createSignal<DraftNotice>();
    [this.restoring, this.setRestoring] = createSignal(0);
    this.createURL = options.createURL ?? ((blob) => URL.createObjectURL(blob));
    this.revokeURL = options.revokeURL ?? ((url) => URL.revokeObjectURL(url));
  }

  /** Every attachment is uploaded, and none is still being restored. */
  ready(): boolean {
    return this.restoring() === 0 && this.items().every((item) => item.info);
  }

  /** The uploaded attachments' ids, in the strip's order. */
  ids(): string[] {
    return this.items().flatMap((item) => (item.info ? [item.info.id] : []));
  }

  /** Images staged before the model changed to one that takes none. */
  blocked(): string | undefined {
    const model = this.options.imageRejectingModel();
    return model && this.items().some((item) => item.image) ? imagesBlocked(model) : undefined;
  }

  dismissNotice(): void {
    this.setNotice(undefined);
  }

  /** Adds files from the picker, a paste, or a drop. */
  add(files: readonly File[]): void {
    if (files.length === 0 || this.disposed) return;
    this.setNotice(undefined);
    const room = MAX_ATTACHMENTS - this.items().length;
    if (files.length > room) this.refuse(REFUSAL.count);
    for (const file of files.slice(0, Math.max(0, room))) {
      const key = `draft-${++keys}`;
      const filename = file.name || "Pasted file";
      this.setItems((items) => [...items, { key, filename, size: file.size, image: false, busy: true }]);
      this.checking = this.checking
        .then(() => this.check(key, file, filename))
        .catch(() => {
          if (!this.present(key)) return;
          this.remove(key);
          this.refuse(REFUSAL.type);
        });
    }
  }

  /** Puts a queued message's attachments back in the strip. */
  async restore(ids: readonly string[]): Promise<void> {
    const have = new Set(this.ids());
    const wanted = ids.filter((id) => !have.has(id));
    if (wanted.length === 0 || this.disposed) return;
    this.setRestoring((count) => count + 1);
    let resolved: AttachmentInfo[];
    try {
      resolved = (await this.api.resolveAttachments(this.session, wanted)).attachments;
    } catch (error) {
      if (!this.disposed && !this.options.fail(error))
        this.refuse("The queued attachments couldn’t be restored.");
      return;
    } finally {
      this.setRestoring((count) => count - 1);
    }
    if (this.disposed) return;
    // The same limits as a file added here: eight, and 20 MB in all.
    let count = this.items().length;
    let total = this.items().reduce((sum, item) => sum + item.size, 0);
    const restored: DraftAttachment[] = [];
    for (const info of resolved) {
      if (count >= MAX_ATTACHMENTS) {
        this.refuse(REFUSAL.count);
        break;
      }
      if (total + info.size > MAX_TOTAL_BYTES) {
        this.refuse(REFUSAL.total);
        break;
      }
      count += 1;
      total += info.size;
      restored.push({
        key: `draft-${++keys}`,
        filename: info.filename,
        size: info.size,
        image: info.mediaType.startsWith("image/"),
        info,
        busy: false,
      });
    }
    this.setItems((items) => [...items, ...restored]);
  }

  remove(key: string): void {
    this.uploads.get(key)?.abort();
    this.uploads.delete(key);
    this.files.delete(key);
    const item = this.items().find((value) => value.key === key);
    if (item?.preview) this.revokeURL(item.preview);
    this.setItems((items) => items.filter((value) => value.key !== key));
  }

  retry(key: string): void {
    const file = this.files.get(key);
    const item = this.items().find((value) => value.key === key);
    if (!file || !item || item.busy) return;
    void this.upload(key, file, item.filename);
  }

  /** Clears what was sent: the text and the attachments the server took. */
  acknowledge(ids: readonly string[]): void {
    const sent = new Set(ids);
    batch(() => {
      for (const item of this.items()) if (item.info && sent.has(item.info.id)) this.remove(item.key);
      this.setText("");
    });
  }

  /** Stops checks and uploads and frees thumbnails when the page closes. */
  dispose(): void {
    this.disposed = true;
    for (const controller of this.uploads.values()) controller.abort();
    this.uploads.clear();
    this.files.clear();
    for (const item of this.items()) if (item.preview) this.revokeURL(item.preview);
    this.setItems([]);
  }

  private update(key: string, change: Partial<DraftAttachment>): boolean {
    let found = false;
    this.setItems((items) =>
      items.map((item) => {
        if (item.key !== key) return item;
        found = true;
        return { ...item, ...change };
      }),
    );
    return found;
  }

  private present(key: string): boolean {
    return !this.disposed && this.items().some((item) => item.key === key);
  }

  private refuse(text: string, tone: DraftNotice["tone"] = "error"): void {
    this.setNotice({ tone, text });
  }

  private async check(key: string, file: File, filename: string): Promise<void> {
    let prepared: Prepared;
    try {
      prepared = await prepare(file, filename, this.options.decode);
    } catch (error) {
      if (!this.present(key)) return;
      this.remove(key);
      this.refuse(error instanceof AttachmentRefused ? error.message : REFUSAL.type);
      return;
    }
    if (!this.present(key)) return;
    const model = prepared.image ? this.options.imageRejectingModel() : undefined;
    if (model) {
      this.remove(key);
      this.refuse(imageRefusal(model, prepared.filename), "warning");
      return;
    }
    const position = this.items().findIndex((item) => item.key === key);
    const before = this.items()
      .slice(0, position)
      .reduce((sum, item) => sum + item.size, 0);
    if (before + prepared.file.size > MAX_TOTAL_BYTES) {
      this.remove(key);
      this.refuse(REFUSAL.total);
      return;
    }
    this.files.set(key, prepared.file);
    this.update(key, {
      filename: prepared.filename,
      size: prepared.file.size,
      image: prepared.image,
      preview: prepared.image ? this.createURL(prepared.file) : undefined,
    });
    void this.upload(key, prepared.file, prepared.filename);
  }

  private async upload(key: string, file: Blob, filename: string): Promise<void> {
    if (!this.present(key)) return;
    const controller = new AbortController();
    this.uploads.get(key)?.abort();
    this.uploads.set(key, controller);
    this.update(key, { busy: true, error: undefined });
    try {
      const info = await this.api.uploadAttachment(this.session, file, filename, controller.signal);
      if (controller.signal.aborted) return;
      this.files.delete(key);
      this.update(key, { info, busy: false, filename: info.filename, size: info.size });
    } catch (error) {
      if (controller.signal.aborted) return;
      // The server refused the file itself; sending it again won't help.
      if (error instanceof ApiError && (error.status === 400 || error.status === 413)) {
        this.remove(key);
        this.refuse(error.status === 413 ? REFUSAL.size : REFUSAL.type);
        return;
      }
      this.options.fail(error);
      this.update(key, { busy: false, error: error instanceof Error ? error.message : "Didn’t upload." });
    } finally {
      if (this.uploads.get(key) === controller) this.uploads.delete(key);
    }
  }
}
