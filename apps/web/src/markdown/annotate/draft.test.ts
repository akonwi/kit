import { expect, describe as suite, test } from "bun:test";
import { createRoot } from "solid-js";
import { splitAnnotations } from "./describe";
import { DiagramAnnotations, DiagramAnnotator, joinAnnotations, MAX_DIAGRAM_ANNOTATIONS } from "./draft";

/** Drawn as testdata/mermaid/flowchart-labeled-branches-merge-again.txt. */
const flowchart = `flowchart TD
A{Valid?} -- yes --> B[Save]
A -- no --> C[Reject]
B --> D[Done]
C --> D`;

const inRoot = <T>(body: () => T): T =>
  createRoot((dispose) => {
    const value = body();
    dispose();
    return value;
  });

suite("waiting diagram annotations", () => {
  test("each names its part, and they go as blocks after the message", () =>
    inRoot(() => {
      const draft = new DiagramAnnotations();
      expect(draft.add("Retry first.", flowchart, { target: "node", index: 2 }, "n1")).toBe(true);
      expect(draft.add("Say why.", flowchart, { target: "edge", index: 1 }, "n2")).toBe(true);
      expect(draft.items().map((item) => item.summary)).toEqual([
        'node C "Reject" (box)',
        "edge A -->|no| C",
      ]);
      expect(draft.parts(flowchart)).toEqual([
        { target: "node", index: 2 },
        { target: "edge", index: 1 },
      ]);
      const text = joinAnnotations("Revise the flow.", draft.messageText());
      expect(text.startsWith('Revise the flow.\n\n<diagram-annotation n="1">\nComment: Retry first.\n')).toBe(
        true,
      );
      expect(splitAnnotations(text).annotations.map((sent) => sent.target)).toEqual([
        'node C "Reject" (box)',
        "edge A -->|no| C",
      ]);
      expect(joinAnnotations("  ", draft.messageText())).toBe(draft.messageText());
      expect(joinAnnotations("Hi", "")).toBe("Hi");
    }));

  test("a part the diagram lacks isn't added, and says why", () =>
    inRoot(() => {
      const draft = new DiagramAnnotations();
      expect(draft.add("x", flowchart, { target: "node", index: 9 })).toBe(false);
      expect(draft.items()).toEqual([]);
      expect(draft.error()).toBe("That part of the diagram can’t be annotated.");
    }));

  test("a message holds at most sixteen", () =>
    inRoot(() => {
      const draft = new DiagramAnnotations();
      for (let count = 0; count < MAX_DIAGRAM_ANNOTATIONS; count++)
        expect(draft.add(`${count}`, flowchart, { target: "node", index: 0 })).toBe(true);
      expect(draft.add("more", flowchart, { target: "node", index: 0 })).toBe(false);
      expect(draft.error()).toBe("A message can contain up to 16 diagram annotations.");
      expect(draft.items()).toHaveLength(16);
    }));

  test("the sent ones are cleared once the server has them, and only those", () =>
    inRoot(() => {
      const draft = new DiagramAnnotations();
      draft.add("one", flowchart, { target: "node", index: 0 });
      const sent = draft.items().map((item) => item.id);
      draft.add("two", flowchart, { target: "node", index: 1 });
      draft.acknowledge(sent);
      expect(draft.items().map((item) => item.annotation.comment)).toEqual(["two"]);
    }));
});

suite("Annotate mode", () => {
  test("picks parts only while it's on, and Esc lets go, then leaves", () =>
    inRoot(() => {
      const annotator = new DiagramAnnotator();
      annotator.hover({ target: "node", index: 0 });
      annotator.click({ target: "node", index: 0 });
      expect([annotator.hovered(), annotator.picked()]).toEqual([undefined, undefined]);
      annotator.start();
      annotator.hover({ target: "node", index: 0 });
      annotator.click({ target: "node", index: 0 });
      annotator.setComment("Half written");
      // The same part keeps its comment; another starts a new one.
      annotator.click({ target: "node", index: 0 });
      expect(annotator.comment()).toBe("Half written");
      annotator.click({ target: "edge", index: 1 });
      expect([annotator.picked(), annotator.comment()]).toEqual([{ target: "edge", index: 1 }, ""]);
      annotator.escape();
      expect([annotator.picked(), annotator.annotating()]).toEqual([undefined, true]);
      annotator.escape();
      expect([annotator.annotating(), annotator.hovered()]).toEqual([false, undefined]);
    }));

  test("Comment adds the comment on the picked part and lets it go", () =>
    inRoot(() => {
      const draft = new DiagramAnnotations();
      const annotator = new DiagramAnnotator();
      annotator.start();
      expect(annotator.commit(draft, flowchart)).toBe(false);
      annotator.click({ target: "node", index: 2 });
      annotator.setComment("  \n");
      expect(annotator.commit(draft, flowchart)).toBe(false);
      annotator.setComment("Say why it was rejected.");
      expect(annotator.commit(draft, flowchart)).toBe(true);
      expect(draft.items().map((item) => [item.summary, item.annotation.comment])).toEqual([
        ['node C "Reject" (box)', "Say why it was rejected."],
      ]);
      expect([annotator.picked(), annotator.comment(), annotator.annotating()]).toEqual([
        undefined,
        "",
        true,
      ]);
    }));
});
