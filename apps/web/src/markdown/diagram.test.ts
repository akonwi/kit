import { describe, expect, test } from "bun:test";
import {
  drawDiagram,
  keepAttribute,
  notDrawnReason,
  refersWithin,
  renderOptions,
  styleDeclarations,
  withoutInlineStyles,
} from "./diagram";
import * as pele from "./pele";

function failure(source: string): unknown {
  try {
    drawDiagram(pele, source, renderOptions("system-ui"));
  } catch (error) {
    return error;
  }
  throw new Error("drew");
}

describe("notDrawnReason", () => {
  test("names a kind the browser doesn't draw, as the Mac does", () => {
    expect(notDrawnReason(failure("classDiagram\n  Session <|-- Subagent"), "")).toBe(
      "classDiagram diagrams aren’t drawn yet",
    );
  });

  test("tells an empty fence from one naming no diagram", () => {
    expect(notDrawnReason(failure(""), "")).toBe("the diagram is empty");
    expect(notDrawnReason(failure("hello world"), "hello world")).toBe(
      "the first line names no diagram type",
    );
  });

  test("points at the line that doesn't parse", () => {
    const source = "sequenceDiagram\n  A->>: hi";
    expect(notDrawnReason(failure(source), source)).toBe("line 2 doesn’t parse: A->>: hi");
  });

  test("refuses what Kit's renderer refuses for size, in its words", () => {
    const chain = (count: number) =>
      Array.from({ length: count }, (_, i) => `  n${i} --> n${i + 1}`).join("\n");
    const cases: [string, string][] = [
      [`flowchart TD\n%% ${"é".repeat(8200)}\n  a --> b`, "the source is larger than 16 KiB"],
      [`flowchart TD\n${Array.from({ length: 61 }, (_, i) => `  n${i}`).join("\n")}`, "more than 60 nodes"],
      [
        `flowchart TD\n${Array.from({ length: 121 }, (_, i) => `  a${i % 9} --> b${i % 13}`).join("\n")}`,
        "more than 120 edges",
      ],
      [
        `flowchart TD\n${Array.from({ length: 21 }, (_, i) => `  subgraph s${i}\n    x${i}\n  end`).join("\n")}`,
        "more than 20 subgraphs",
      ],
      [
        `sequenceDiagram\n${Array.from({ length: 25 }, (_, i) => `  participant P${i}`).join("\n")}`,
        "more than 24 participants",
      ],
      [`sequenceDiagram\n${"  A->>B: hi\n".repeat(201)}`, "more than 200 statements"],
      [
        `flowchart TD\n  ${Array.from({ length: 40 }, (_, i) => `a${i}`).join(" & ")} --> ${Array.from({ length: 20 }, (_, i) => `b${i}`).join(" & ")}`,
        "more than 120 edges",
      ],
    ];
    for (const [source, reason] of cases) expect(notDrawnReason(failure(source), source)).toBe(reason);
    expect(() => drawDiagram(pele, `flowchart TD\n${chain(59)}`, renderOptions("system-ui"))).not.toThrow();
  });

  test("gives a general reason for anything else", () => {
    expect(notDrawnReason(new Error("boom"), "flowchart LR")).toBe("it couldn’t be drawn");
  });
});

describe("styleDeclarations", () => {
  test("splits only at the semicolons and colons between declarations", () => {
    expect(
      styleDeclarations(
        'max-width:100%;--_bg:var(--pele-bg,#fff);font-family:var(--pele-font,"A;B", system-ui);rx:var(--_r);',
      ),
    ).toEqual([
      ["max-width", "100%"],
      ["--_bg", "var(--pele-bg,#fff)"],
      ["font-family", 'var(--pele-font,"A;B", system-ui)'],
      ["rx", "var(--_r)"],
    ]);
  });
});

describe("keepAttribute", () => {
  test("keeps drawing attributes and links within the drawing only", () => {
    expect(keepAttribute("fill", "var(--_s)")).toBe(true);
    expect(keepAttribute("marker-end", "url(#kd1-arrow)")).toBe(true);
    expect(keepAttribute("href", "#kd1-arrow")).toBe(true);
    expect(keepAttribute("href", "https://example.com")).toBe(false);
    expect(keepAttribute("xlink:href", "javascript:alert(1)")).toBe(false);
    expect(keepAttribute("onclick", "alert(1)")).toBe(false);
    expect(keepAttribute("data-id", "javascript:alert(1)")).toBe(false);
    expect(keepAttribute("fill", "url(https://example.com/x.svg#p)")).toBe(false);
    expect(keepAttribute("filter", "url( 'data:image/svg+xml,<svg/>' )")).toBe(false);
    expect(keepAttribute("xml:base", "https://example.com/")).toBe(false);
  });
});

describe("refersWithin", () => {
  test("allows only url() of the drawing's own ids, and no other resource", () => {
    expect(refersWithin("var(--pele-bg,#fff)")).toBe(true);
    expect(refersWithin("url(#kd2-grad) url('#kd2-mask')")).toBe(true);
    expect(refersWithin("url(#a) url(/v1/sessions)")).toBe(false);
    expect(refersWithin("image-set('x.png' 1x)")).toBe(false);
    expect(refersWithin('url("https://example.com/beacon")')).toBe(false);
  });
});

describe("withoutInlineStyles", () => {
  test("renames style attributes, quoted either way, past a > inside a value", () => {
    expect(
      withoutInlineStyles(
        `<svg aria-label="a > b" style="a:b"><rect x='1' style='rx:2'/><text>a style="c" > d</text><g\n  style = "e:f"/></svg>`,
      ),
    ).toBe(
      `<svg aria-label="a > b" data-pele-style="a:b"><rect x='1' data-pele-style='rx:2'/><text>a style="c" > d</text><g\n  data-pele-style = "e:f"/></svg>`,
    );
  });
});
