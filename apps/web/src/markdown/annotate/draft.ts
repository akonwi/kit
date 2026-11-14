// The diagram annotations waiting in a session's composer, and a diagram's
// Annotate mode, as the Mac and iPhone hold them (ADR 0046's annotations).
// Nothing is stored on the server until the message is sent: each comment
// becomes a block of text after the message's own.
import { type Accessor, batch, createContext, createSignal, type Setter } from "solid-js";
import { annotationText, type DiagramAnnotation, describe, nonce, type Part, summary } from "./describe";

/** The most annotations one message holds. */
export const MAX_DIAGRAM_ANNOTATIONS = 16;

export interface DiagramItem {
  id: string;
  annotation: DiagramAnnotation;
  /** The part, as the model's text names it. */
  summary: string;
  /** What Kit read from the diagram about the part. */
  facts: string[];
}

let items = 0;

export class DiagramAnnotations {
  readonly items: Accessor<DiagramItem[]>;
  readonly error: Accessor<string | undefined>;
  private readonly setItems: Setter<DiagramItem[]>;
  readonly setError: Setter<string | undefined>;

  constructor() {
    [this.items, this.setItems] = createSignal<DiagramItem[]>([]);
    [this.error, this.setError] = createSignal<string>();
  }

  /** Adds a comment on a part of a diagram's source; false when the message holds all it can or the diagram has no such part. */
  add(comment: string, source: string, part: Part, tag: string = nonce()): boolean {
    if (this.items().length >= MAX_DIAGRAM_ANNOTATIONS) {
      this.setError(`A message can contain up to ${MAX_DIAGRAM_ANNOTATIONS} diagram annotations.`);
      return false;
    }
    let facts: string[];
    let named: string;
    try {
      facts = describe(source, part);
      named = summary(source, part);
    } catch {
      this.setError("That part of the diagram can’t be annotated.");
      return false;
    }
    batch(() => {
      this.setError(undefined);
      this.setItems((list) => [
        ...list,
        {
          id: `diagram-${++items}`,
          annotation: { comment, source, part, nonce: tag },
          summary: named,
          facts,
        },
      ]);
    });
    return true;
  }

  remove(id: string): void {
    batch(() => {
      this.setItems((list) => list.filter((item) => item.id !== id));
      if (this.items().length === 0) this.setError(undefined);
    });
  }

  /** The parts of source's diagram that have a waiting annotation. */
  parts(source: string): Part[] {
    return this.items()
      .filter((item) => item.annotation.source === source)
      .map((item) => item.annotation.part);
  }

  /** The blocks the model gets after the message's text, or "" for none. */
  messageText(): string {
    try {
      return annotationText(this.items().map((item) => item.annotation));
    } catch {
      return "";
    }
  }

  /** Removes the annotations a sent message carried. */
  acknowledge(ids: readonly string[]): void {
    const sent = new Set(ids);
    batch(() => {
      this.setItems((list) => list.filter((item) => !sent.has(item.id)));
      if (this.items().length === 0) this.setError(undefined);
    });
  }
}

/** The message's text, then the blocks after it. */
export function joinAnnotations(text: string, blocks: string): string {
  if (!blocks) return text;
  return text.trim() === "" ? blocks : `${text}\n\n${blocks}`;
}

/** The session's diagram annotations, where a diagram can be annotated. */
export const DiagramAnnotationsContext = createContext<DiagramAnnotations>();

/** A diagram's Annotate mode: the part under the pointer, the part picked, and the comment being written on it. */
export class DiagramAnnotator {
  readonly annotating: Accessor<boolean>;
  readonly hovered: Accessor<Part | undefined>;
  readonly picked: Accessor<Part | undefined>;
  readonly comment: Accessor<string>;
  readonly setComment: Setter<string>;
  private readonly setAnnotating: Setter<boolean>;
  private readonly setHovered: Setter<Part | undefined>;
  private readonly setPicked: Setter<Part | undefined>;

  constructor() {
    [this.annotating, this.setAnnotating] = createSignal(false);
    [this.hovered, this.setHovered] = createSignal<Part>();
    [this.picked, this.setPicked] = createSignal<Part>();
    [this.comment, this.setComment] = createSignal("");
  }

  start(): void {
    this.setAnnotating(true);
  }

  stop(): void {
    batch(() => {
      this.setAnnotating(false);
      this.setHovered(undefined);
      this.deselect();
    });
  }

  toggle(): void {
    if (this.annotating()) this.stop();
    else this.start();
  }

  hover(part: Part | undefined): void {
    const next = this.annotating() ? part : undefined;
    if (!same(next, this.hovered())) this.setHovered(next);
  }

  /** Picks the part clicked, or lets go of the picked one when the click lands on nothing. A new part starts a new comment. */
  click(part: Part | undefined): void {
    if (!this.annotating()) return;
    batch(() => {
      if (!same(part, this.picked())) this.setComment("");
      this.setPicked(part);
    });
  }

  deselect(): void {
    batch(() => {
      this.setPicked(undefined);
      this.setComment("");
    });
  }

  /** Esc: lets go of the picked part, else leaves the mode. */
  escape(): void {
    if (this.picked()) this.deselect();
    else this.stop();
  }

  /** Whether the comment has something to add. */
  canCommit(): boolean {
    return this.picked() !== undefined && this.comment().trim() !== "";
  }

  /** Adds the comment on the picked part to draft and lets go of the part; false, keeping both, when it isn't added. */
  commit(draft: DiagramAnnotations, source: string): boolean {
    const part = this.picked();
    if (!part || !this.canCommit() || !draft.add(this.comment(), source, part)) return false;
    this.deselect();
    return true;
  }
}

/** Whether two parts are the same part; nothing is never the same as anything. */
export function same(a: Part | undefined, b: Part | undefined): boolean {
  if (a === undefined || b === undefined) return a === b;
  return a.target === b.target && a.index === b.index;
}
