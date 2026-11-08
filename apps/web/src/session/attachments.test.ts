import { describe, expect, test } from "bun:test";
import { type Api, ApiError, NetworkError } from "../api/client";
import type { AttachmentInfo } from "../api/types";
import {
  AttachmentRefused,
  type Decoder,
  Draft,
  type DraftOptions,
  isText,
  prepare,
  REFUSAL,
  sniffImage,
} from "./attachments";

const PNG = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
const png = (name = "shot.png", size = 64) =>
  new File([new Uint8Array([...PNG, ...new Array(Math.max(0, size - PNG.length)).fill(1)])], name, {
    type: "image/png",
  });
const text = (name = "trace.log", body = "12:04:11 stream open\n") =>
  new File([body], name, { type: "text/plain" });

/** A decoder that reports a fixed size and converts to a small PNG. */
function decoder(width = 1170, height = 2532, pngSize = 100): Decoder {
  return async () => ({
    width,
    height,
    png: async () => new Blob([new Uint8Array(pngSize)], { type: "image/png" }),
    close() {},
  });
}

describe("checks", () => {
  test("sniffs the four image formats the server takes", () => {
    expect(sniffImage(new Uint8Array(PNG))).toBe("image/png");
    expect(sniffImage(new Uint8Array([0xff, 0xd8, 0xff, 0xe0]))).toBe("image/jpeg");
    expect(sniffImage(new TextEncoder().encode("GIF89a"))).toBe("image/gif");
    expect(sniffImage(new TextEncoder().encode("RIFF\0\0\0\0WEBPVP8 "))).toBe("image/webp");
    expect(sniffImage(new TextEncoder().encode("hello"))).toBeUndefined();
  });

  test("takes UTF-8 text without NUL", () => {
    expect(isText(new TextEncoder().encode("héllo\n"))).toBe(true);
    expect(isText(new Uint8Array([0x68, 0x00]))).toBe(false);
    expect(isText(new Uint8Array([0xc3, 0x28]))).toBe(false);
  });

  test("passes a supported image and a text file unchanged", async () => {
    const image = png();
    expect(await prepare(image, "shot.png", decoder())).toEqual({
      file: image,
      filename: "shot.png",
      image: true,
    });
    const log = text();
    expect(await prepare(log, "trace.log", decoder())).toEqual({
      file: log,
      filename: "trace.log",
      image: false,
    });
  });

  test("converts an image format the server doesn't take to PNG", async () => {
    const heic = new File([new Uint8Array([0, 0, 0, 0x18, 0x66, 0x74, 0x79, 0x70])], "IMG_0412.HEIC", {
      type: "image/heic",
    });
    const prepared = await prepare(heic, "IMG_0412.HEIC", decoder());
    expect(prepared.filename).toBe("IMG_0412.png");
    expect(prepared.image).toBe(true);
    expect(prepared.file.type).toBe("image/png");
  });

  test("refuses with the limit it breaks", async () => {
    const refusal = (file: File, decode = decoder()) =>
      prepare(file, file.name, decode).then(
        () => "attached",
        (error: unknown) => (error instanceof AttachmentRefused ? error.message : String(error)),
      );
    expect(await refusal(new File([], "empty.txt"))).toBe(REFUSAL.size);
    expect(await refusal(png("huge.png", (10 << 20) + 1))).toBe(REFUSAL.size);
    expect(await refusal(png(), decoder(8193, 10))).toBe(REFUSAL.dimensions);
    expect(await refusal(png(), decoder(4000, 3001))).toBe(REFUSAL.dimensions);
    expect(await refusal(new File([new Uint8Array([0, 1, 2])], "data.bin"))).toBe(REFUSAL.type);
    expect(await refusal(text("big.log", "a".repeat((1 << 20) + 1)))).toBe(REFUSAL.type);
    expect(await refusal(png(), async () => undefined)).toBe(REFUSAL.type);
    const heic = new File([new Uint8Array([0, 0, 0, 0x18])], "x.heic", { type: "image/heic" });
    expect(await refusal(heic, decoder(100, 100, (10 << 20) + 1))).toBe(REFUSAL.converted);
  });
});

const settle = async () => {
  for (let i = 0; i < 10; i += 1) await new Promise((resolve) => setTimeout(resolve, 0));
};

function info(filename: string, id = `attachment_${filename.replace(/\W/g, "")}`, size = 64): AttachmentInfo {
  return {
    id,
    sessionId: "s1",
    filename,
    mediaType: filename.endsWith(".png") ? "image/png" : "text/plain",
    size,
    sha256: "",
    createdAt: "",
  };
}

/** An API whose uploads answer in turn from `results`, then succeed. */
function uploads(results: (AttachmentInfo | Error)[] = []) {
  const sent: string[] = [];
  const api = {
    async uploadAttachment(_session: string, file: Blob, filename: string) {
      sent.push(filename);
      const next = results.shift();
      if (next instanceof Error) throw next;
      return next ?? info(filename, undefined, file.size);
    },
    async resolveAttachments(_session: string, ids: string[]) {
      return { attachments: ids.map((id) => ({ ...info(`${id}.png`, id) })) };
    },
  } as unknown as Api;
  return { api, sent };
}

function draft(api: Api, extra: Partial<DraftOptions> = {}) {
  let urls = 0;
  return new Draft(api, "s1", {
    imageRejectingModel: () => undefined,
    fail: () => false,
    decode: decoder(),
    createURL: () => `blob:${++urls}`,
    revokeURL: () => {},
    ...extra,
  });
}

describe("Draft", () => {
  test("uploads each file as it's added and sends their ids", async () => {
    const { api, sent } = uploads();
    const value = draft(api);
    value.add([png(), text()]);
    expect(value.items().map((item) => [item.filename, item.busy])).toEqual([
      ["shot.png", true],
      ["trace.log", true],
    ]);
    expect(value.ready()).toBe(false);
    await settle();
    expect(sent).toEqual(["shot.png", "trace.log"]);
    expect(value.ready()).toBe(true);
    expect(value.ids()).toEqual(["attachment_shotpng", "attachment_tracelog"]);
    expect(value.items().map((item) => [item.image, item.preview])).toEqual([
      [true, "blob:1"],
      [false, undefined],
    ]);
  });

  test("attaches eight at most and says so", async () => {
    const { api } = uploads();
    const value = draft(api);
    value.add(Array.from({ length: 9 }, (_, i) => text(`${i}.txt`)));
    await settle();
    expect(value.items()).toHaveLength(8);
    expect(value.notice()).toEqual({ tone: "error", text: REFUSAL.count });
  });

  test("leaves out an image the model can't take, and attaches the rest", async () => {
    const { api, sent } = uploads();
    const value = draft(api, { imageRejectingModel: () => "GPT-5.5" });
    value.add([png("Screenshot.png"), text()]);
    await settle();
    expect(sent).toEqual(["trace.log"]);
    expect(value.items().map((item) => item.filename)).toEqual(["trace.log"]);
    expect(value.notice()).toEqual({
      tone: "warning",
      text: "GPT-5.5 doesn’t accept images, so Screenshot.png wasn’t attached.",
    });
  });

  test("blocks sending images staged before the model changed", async () => {
    const { api } = uploads();
    let model: string | undefined;
    const value = draft(api, { imageRejectingModel: () => model });
    value.add([png()]);
    await settle();
    expect(value.blocked()).toBeUndefined();
    model = "Kimi K2.6";
    expect(value.blocked()).toBe("Kimi K2.6 doesn’t accept images. Remove them to send.");
  });

  test("keeps a file that didn't upload until Retry sends it", async () => {
    const { api, sent } = uploads([new NetworkError("Kit’s server can’t be reached.")]);
    const value = draft(api);
    value.add([text()]);
    await settle();
    expect(value.items()[0]).toMatchObject({ busy: false, error: "Kit’s server can’t be reached." });
    expect(value.ready()).toBe(false);
    value.retry(value.items()[0]?.key ?? "");
    await settle();
    expect(sent).toEqual(["trace.log", "trace.log"]);
    expect(value.items()[0]).toMatchObject({ busy: false, error: undefined });
    expect(value.ready()).toBe(true);
  });

  test("drops a file the server refuses, with the reason", async () => {
    const { api } = uploads([new ApiError(413, "limit_exceeded", "attachment is too large")]);
    const value = draft(api);
    value.add([text()]);
    await settle();
    expect(value.items()).toEqual([]);
    expect(value.notice()).toEqual({ tone: "error", text: REFUSAL.size });
  });

  test("keeps the total at 20 MB", async () => {
    const { api } = uploads();
    const value = draft(api);
    value.add([png("a.png", 9 << 20), png("b.png", 9 << 20), png("c.png", 3 << 20)]);
    await settle();
    expect(value.items().map((item) => item.filename)).toEqual(["a.png", "b.png"]);
    expect(value.notice()).toEqual({ tone: "error", text: REFUSAL.total });
  });

  test("clears the text and what was sent", async () => {
    const { api } = uploads();
    const value = draft(api);
    value.setText("Same crash after the fix");
    value.add([png(), text()]);
    await settle();
    const ids = value.ids();
    value.add([text("late.txt")]);
    value.acknowledge(ids);
    expect(value.text()).toBe("");
    expect(value.items().map((item) => item.filename)).toEqual(["late.txt"]);
  });

  test("restores a queued message's attachments as uploaded", async () => {
    const { api } = uploads();
    const value = draft(api);
    await value.restore(["attachment_a", "attachment_b"]);
    expect(value.items().map((item) => [item.filename, item.image, item.busy])).toEqual([
      ["attachment_a.png", true, false],
      ["attachment_b.png", true, false],
    ]);
    expect(value.ids()).toEqual(["attachment_a", "attachment_b"]);
  });

  test("holds Send while restoring, and keeps restored files within the limits", async () => {
    let answer: () => void = () => {};
    const api = {
      resolveAttachments: (_session: string, ids: string[]) =>
        new Promise((resolve) => {
          answer = () => resolve({ attachments: ids.map((id) => info(`${id}.png`, id, 9 << 20)) });
        }),
    } as unknown as Api;
    const value = draft(api);
    const restoring = value.restore(["attachment_a", "attachment_b", "attachment_c"]);
    expect(value.ready()).toBe(false);
    answer();
    await restoring;
    expect(value.ready()).toBe(true);
    expect(value.ids()).toEqual(["attachment_a", "attachment_b"]);
    expect(value.notice()).toEqual({ tone: "error", text: REFUSAL.total });
  });

  test("stops checking and uploading when the page closes", async () => {
    const { api, sent } = uploads();
    let urls = 0;
    const value = draft(api, { createURL: () => `blob:${++urls}` });
    value.add([png(), text()]);
    value.dispose();
    await settle();
    expect(sent).toEqual([]);
    expect(urls).toBe(0);
    expect(value.items()).toEqual([]);
  });
});
