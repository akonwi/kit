// Server-sent event framing for streams read with fetch, which can send the
// bearer token that EventSource cannot (ADR 0059).

export interface Frame {
  event: string;
  data: string;
  id?: string;
}

/** Bytes one frame may hold before the stream is treated as malformed. */
export const FRAME_LIMIT = 2 * 1024 * 1024;

export class FrameTooLarge extends Error {
  override name = "FrameTooLarge";
}

/** Splits decoded stream text into frames; feed it chunks as they arrive. */
export class SSEParser {
  private buffer = "";
  private event = "";
  private data: string[] = [];
  private id: string | undefined;
  private size = 0;

  push(chunk: string): Frame[] {
    this.buffer += chunk;
    const frames: Frame[] = [];
    for (;;) {
      const newline = this.buffer.indexOf("\n");
      if (newline < 0) break;
      let line = this.buffer.slice(0, newline);
      this.buffer = this.buffer.slice(newline + 1);
      if (line.endsWith("\r")) line = line.slice(0, -1);
      const frame = this.line(line);
      if (frame) frames.push(frame);
    }
    if (this.buffer.length + this.size > FRAME_LIMIT)
      throw new FrameTooLarge("A stream record was too large.");
    return frames;
  }

  private line(line: string): Frame | undefined {
    if (line === "") {
      const frame =
        this.data.length > 0
          ? { event: this.event || "message", data: this.data.join("\n"), id: this.id }
          : undefined;
      this.event = "";
      this.data = [];
      this.size = 0;
      return frame;
    }
    if (line.startsWith(":")) return undefined;
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    switch (field) {
      case "event":
        this.event = value;
        break;
      case "data":
        this.data.push(value);
        this.size += value.length;
        break;
      case "id":
        this.id = value;
        break;
    }
    return undefined;
  }
}
