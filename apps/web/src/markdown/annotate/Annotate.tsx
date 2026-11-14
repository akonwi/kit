// Annotate mode over a diagram Pele drew: the pointer outlines the part it's
// over, a click picks it, and a comment box under the drawing adds a comment
// on it to the message. Parts with a waiting comment carry a mark. The
// drawing is replaced when it's laid out again, so the mode attaches to each
// drawing in turn.
import { createEffect, createSignal, onCleanup, Show } from "solid-js";
import { Icon } from "../../ui/icons";
import { type Part, summary } from "./describe";
import { type DiagramAnnotations, type DiagramAnnotator, same } from "./draft";
import { drawnElement, drawnOf, drawnParts, type PeleModel, partFor } from "./drawing";
import type { Diagram } from "./parse";

const SVG = "http://www.w3.org/2000/svg";

/** Lines a pointer can hardly land on, given a wider band that takes it in Annotate mode. */
const THIN = ".pele-edge path:not(.pele-marker), .pele-message path, .pele-frame > rect:first-child";

export interface AnnotatedDrawing {
  source: string;
  kit: Diagram;
  pele: PeleModel;
  annotator: DiagramAnnotator;
  draft: DiagramAnnotations;
}

/**
 * Wires Annotate mode into one drawing. Call it in a reactive owner that
 * ends when the drawing is replaced or goes.
 */
export function annotateDrawing(svg: SVGSVGElement, props: AnnotatedDrawing): void {
  const parts = drawnParts(svg, props.kit, props.pele);
  const elements = (part: Part | undefined) =>
    part ? parts.filter((entry) => same(entry.part, part)).map((entry) => entry.element) : [];
  const partAt = (target: EventTarget | null) => {
    const element = drawnElement(target, svg);
    const drawn = element && drawnOf(element, svg);
    return drawn && partFor(drawn, props.kit, props.pele);
  };

  const move = (event: PointerEvent) => props.annotator.hover(partAt(event.target));
  const leave = () => props.annotator.hover(undefined);
  const click = (event: MouseEvent) => {
    if (!props.annotator.annotating()) return;
    event.preventDefault();
    props.annotator.click(partAt(event.target));
  };
  svg.addEventListener("pointermove", move);
  svg.addEventListener("pointerleave", leave);
  svg.addEventListener("click", click);
  onCleanup(() => {
    svg.removeEventListener("pointermove", move);
    svg.removeEventListener("pointerleave", leave);
    svg.removeEventListener("click", click);
  });

  // The mode itself: a pointer over the parts, and wide bands over thin lines.
  createEffect(() => {
    if (!props.annotator.annotating()) return;
    svg.setAttribute("data-annotating", "");
    const bands: Element[] = [];
    for (const line of svg.querySelectorAll(THIN)) {
      const band = line.cloneNode(false) as Element;
      band.removeAttribute("class");
      band.removeAttribute("style");
      band.setAttribute("class", "kit-hit");
      line.after(band);
      bands.push(band);
    }
    onCleanup(() => {
      svg.removeAttribute("data-annotating");
      for (const band of bands) band.remove();
    });
  });

  const mark = (className: string, part: () => Part | undefined) =>
    createEffect(() => {
      const found = elements(part());
      for (const element of found) element.classList.add(className);
      onCleanup(() => {
        for (const element of found) element.classList.remove(className);
      });
    });
  mark("kit-hovered", () =>
    same(props.annotator.hovered(), props.annotator.picked()) ? undefined : props.annotator.hovered(),
  );
  mark("kit-picked", () => props.annotator.picked());

  // A dot at the top right of each part with a waiting comment.
  createEffect(() => {
    const dots: Element[] = [];
    for (const part of props.draft.parts(props.source)) {
      const found = elements(part);
      const host = (found.find((element) => element.classList.contains("pele-edge-label")) ?? found[0]) as
        | SVGGraphicsElement
        | undefined;
      if (!host?.getBBox) continue;
      const box = host.getBBox();
      const dot = document.createElementNS(SVG, "circle");
      dot.setAttribute("class", "kit-mark");
      dot.setAttribute("cx", String(box.x + box.width));
      dot.setAttribute("cy", String(box.y));
      dot.setAttribute("r", "4.5");
      host.append(dot);
      dots.push(dot);
    }
    onCleanup(() => {
      for (const dot of dots) dot.remove();
    });
  });
}

/** Annotate in a diagram's header, or Done while the mode is on. */
export function AnnotateButton(props: { annotator: DiagramAnnotator }) {
  return (
    <button
      type="button"
      class="fence-action"
      aria-pressed={props.annotator.annotating()}
      data-active={props.annotator.annotating() || undefined}
      title={props.annotator.annotating() ? "Stop annotating" : "Annotate parts of the diagram"}
      onClick={() => props.annotator.toggle()}
    >
      <Icon name="cursorClick" size={12} />
      {props.annotator.annotating() ? "Done" : "Annotate"}
    </button>
  );
}

/** The box under a drawing where the picked part's comment is written. */
export function DiagramCommentBox(props: {
  annotator: DiagramAnnotator;
  draft: DiagramAnnotations;
  source: string;
}) {
  const picked = () => props.annotator.picked();
  return (
    <Show when={picked()} keyed>
      {(part) => (
        <CommentForm part={part} annotator={props.annotator} draft={props.draft} source={props.source} />
      )}
    </Show>
  );
}

function CommentForm(props: {
  part: Part;
  annotator: DiagramAnnotator;
  draft: DiagramAnnotations;
  source: string;
}) {
  const named = () => {
    try {
      return summary(props.source, props.part);
    } catch {
      return "";
    }
  };
  const [field, setField] = createSignal<HTMLTextAreaElement>();
  createEffect(() => field()?.focus({ preventScroll: true }));
  const save = () => props.annotator.commit(props.draft, props.source);
  return (
    <form
      class="comment-editor diagram-comment"
      onSubmit={(event) => {
        event.preventDefault();
        save();
      }}
    >
      <p class="diagram-comment-target mono">{named()}</p>
      <textarea
        ref={setField}
        rows={2}
        value={props.annotator.comment()}
        placeholder="Comment on this part…"
        aria-label={`Comment on ${named()}`}
        onInput={(event) => props.annotator.setComment(event.currentTarget.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            save();
          }
          if (event.key === "Escape") {
            event.preventDefault();
            props.annotator.deselect();
          }
        }}
      />
      <Show when={props.draft.error()}>
        {(message) => (
          <p class="error small" role="alert">
            {message()}
          </p>
        )}
      </Show>
      <div class="comment-actions">
        <button type="button" onClick={() => props.annotator.deselect()}>
          Cancel
        </button>
        <button type="submit" data-variant="primary" disabled={!props.annotator.canCommit()}>
          Comment
        </button>
      </div>
    </form>
  );
}
