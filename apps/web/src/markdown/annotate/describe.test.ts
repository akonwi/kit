import { expect, describe as suite, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  annotationText,
  block,
  type DiagramAnnotation,
  nonce,
  type Part,
  splitAnnotations,
  summary,
  type Target,
} from "./describe";
import { NotDrawn } from "./text";

const fixtures = join(import.meta.dir, "../../../../../testdata/mermaid");
const fixture = (name: string) => readFileSync(join(fixtures, name), "utf8");

suite("annotation text", () => {
  // Each fixture's `annotate <fixture>.mmd <kind> <index> <nonce>` lines, each
  // followed by a comment, then `===` and the reference's text.
  const cases = readdirSync(join(fixtures, "annotations")).filter((name) => name.endsWith(".txt"));

  test("the fixtures are present", () => {
    expect(cases.length).toBeGreaterThanOrEqual(4);
  });

  for (const name of cases)
    test(`writes the reference's text: ${name}`, () => {
      const content = fixture(join("annotations", name));
      const split = content.indexOf("\n===\n");
      const items: DiagramAnnotation[] = [];
      let comment: string[] = [];
      let pending: Omit<DiagramAnnotation, "comment"> | undefined;
      const finish = () => {
        if (pending) items.push({ ...pending, comment: comment.join("\n") });
        comment = [];
      };
      for (const line of content.slice(0, split).split("\n")) {
        if (line.startsWith("annotate ")) {
          finish();
          const [, file = "", target = "", index = "", nonce = ""] = line.split(" ");
          pending = {
            source: fixture(file),
            part: { target: target as Target, index: Number(index) },
            nonce,
          };
        } else comment.push(line);
      }
      finish();
      let actual: string;
      try {
        actual = `${annotationText(items)}\n`;
      } catch (error) {
        if (!(error instanceof NotDrawn)) throw error;
        actual = `not annotated: ${error.message}\n`;
      }
      expect(actual).toBe(content.slice(split + "\n===\n".length));
    });
});

suite("part summaries", () => {
  // Every part the reference drew, named as the reference names it in the
  // fixture's regions: `x0 y0 x1 y1 box|line kind index summary`.
  const names = readdirSync(fixtures)
    .filter((name) => name.endsWith(".regions"))
    .map((name) => name.slice(0, -".regions".length));

  test("the fixtures are present", () => {
    expect(names.length).toBeGreaterThan(40);
  });

  for (const name of names)
    test(`names each part as the reference does: ${name}`, () => {
      const source = fixture(`${name}.mmd`);
      const expected = new Map<string, string>();
      for (const line of fixture(`${name}.regions`).split("\n").filter(Boolean)) {
        const [, , , , , target = "", index = "", ...rest] = line.split(" ");
        expected.set(`${target} ${index}`, rest.join(" "));
      }
      const actual = new Map<string, string>();
      for (const key of expected.keys()) {
        const [target = "", index = ""] = key.split(" ");
        const part: Part = { target: target as Target, index: Number(index) };
        let named: string;
        try {
          named = summary(source, part);
        } catch {
          named = "?";
        }
        actual.set(key, named);
      }
      expect(actual).toEqual(expected);
    });
});

suite("sent annotations", () => {
  const source = "flowchart TD\nA --> B";

  test("split from the message's own text", () => {
    const text = annotationText([
      { comment: "Rename.\nAnd say why.", source, part: { target: "node", index: 0 }, nonce: "n1" },
      { comment: "Why?", source, part: { target: "edge", index: 0 }, nonce: "n2" },
    ]);
    expect(splitAnnotations(`Revise it.\n\n${text}`)).toEqual({
      text: "Revise it.",
      annotations: [
        { number: 1, comment: "Rename.\nAnd say why.", diagram: "flowchart TD", target: "node A (box)" },
        { number: 2, comment: "Why?", diagram: "flowchart TD", target: "edge A --> B" },
      ],
    });
  });

  test("only numbered blocks that end the message are split from it", () => {
    const one = block(1, { comment: "x", source, part: { target: "node", index: 0 }, nonce: "n1" }, 0);
    const two = block(2, { comment: "y", source, part: { target: "node", index: 1 }, nonce: "n2" }, 1);
    const quoted = splitAnnotations(`See:\n\n${one}\n\nWhy?\n\n${one}`);
    expect(quoted.text).toBe(`See:\n\n${one}\n\nWhy?`);
    expect(quoted.annotations).toHaveLength(1);
    for (const kept of [
      `Why?\n\n${one}\n\nmore`,
      `Why?\n\n${two}`,
      `Why? ${one}`,
      `Why?\n\n${one.replace('n="1"', 'n="1e400"')}`,
    ]) {
      expect(splitAnnotations(kept)).toEqual({ text: kept, annotations: [] });
    }
    const both = splitAnnotations(`${one}\n\n${two}\n`);
    expect(both.text).toBe("");
    expect(both.annotations.map((sent) => sent.number)).toEqual([1, 2]);
  });

  test("nonces are 128 random bits in hex", () => {
    expect(nonce()).toMatch(/^[0-9a-f]{32}$/);
    expect(nonce()).not.toBe(nonce());
  });

  test("a block whose fence doesn't close with its nonce stays text", () => {
    const text = `${annotationText([{ comment: "x", source, part: { target: "node", index: 0 }, nonce: "n1" }])}`;
    const broken = text.replace(
      "[/UNTRUSTED_DIAGRAM_CONTENT nonce=n1]",
      "[/UNTRUSTED_DIAGRAM_CONTENT nonce=n2]",
    );
    expect(splitAnnotations(broken)).toEqual({ text: broken, annotations: [] });
  });
});
