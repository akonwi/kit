import { expect, describe as suite, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { renderOptions } from "../diagram";
import { parse, render } from "../pele";
import { summary } from "./describe";
import { type Drawn, type PeleModel, partFor } from "./drawing";
import { cleanSequence, readDiagram } from "./parse";

const fixtures = join(import.meta.dir, "../../../../../testdata/mermaid");

/** The elements Pele drew that stand for parts, read from its SVG as the page's classes and `data-id`s show them. */
function drawnIn(svg: string): { drawn: Drawn; keyword?: string }[] {
  const sequence = /class="pele pele-sequence/.test(svg);
  const found: { drawn: Drawn; keyword?: string }[] = [];
  let frames = 0;
  for (const match of svg.matchAll(/<g class="([^"]*)"(?: data-id="([^"]*)")?/g)) {
    const classes = (match[1] ?? "").split(" ");
    const id = match[2];
    if (classes.includes("pele-frame") && !classes.includes("pele-frame-rect")) {
      const keyword = classes.find((name) => name.startsWith("pele-frame-"))?.slice("pele-frame-".length);
      found.push({ drawn: { kind: "frame", ordinal: frames++ }, keyword });
    } else if (id === undefined) continue;
    else if (classes.includes("pele-note")) found.push({ drawn: { kind: "note", id } });
    else if (classes.includes("pele-node")) found.push({ drawn: { kind: sequence ? "actor" : "node", id } });
    else if (classes.includes("pele-cluster") || classes.includes("pele-cluster-title"))
      found.push({ drawn: { kind: "cluster", id } });
    else if (classes.includes("pele-edge") || classes.includes("pele-edge-label"))
      found.push({ drawn: { kind: sequence ? "message" : "edge", id } });
  }
  return found;
}

suite("Pele's drawing mapped to Kit's parts", () => {
  const names = readdirSync(fixtures)
    .filter((name) => name.endsWith(".regions"))
    .map((name) => name.slice(0, -".regions".length));

  for (const name of names)
    test(`every drawn part is the part Kit reads, and every part is drawn: ${name}`, () => {
      const source = readFileSync(join(fixtures, `${name}.mmd`), "utf8");
      const kit = readDiagram(source);
      let model: PeleModel;
      let svg: string;
      try {
        const options = renderOptions("sans-serif");
        model = parse(source, options) as PeleModel;
        svg = render(model as never, options).svg;
      } catch {
        return; // Pele doesn't draw it; nothing to point at.
      }
      const reached = new Set<string>();
      for (const { drawn, keyword } of drawnIn(svg)) {
        const part = partFor(drawn, kit, model);
        expect({ drawn, part: part !== undefined }).toEqual({ drawn, part: true });
        if (!part) continue;
        reached.add(`${part.target} ${part.index}`);
        // The part is one Kit can describe, and the same thing Pele drew.
        expect(() => summary(source, part)).not.toThrow();
        if (kit.type === "flowchart" && drawn.kind === "edge" && "edges" in model) {
          const edge = model.edges.find((value) => value.id === drawn.id);
          const mine = kit.chart.edges[part.index];
          expect([kit.chart.nodes[mine?.from ?? -1]?.id, kit.chart.nodes[mine?.to ?? -1]?.id]).toEqual([
            edge?.start,
            edge?.end,
          ]);
        }
        if (kit.type === "sequence" && drawn.kind === "frame")
          expect(kit.diagram.events[part.index]?.keyword).toBe(keyword);
        if (
          kit.type === "sequence" &&
          (drawn.kind === "message" || drawn.kind === "note") &&
          "messages" in model
        ) {
          const statement = model.messages.find((value) => `i${value.id}` === drawn.id) as {
            message?: string;
          };
          expect(kit.diagram.events[part.index]?.written || kit.diagram.events[part.index]?.text).toBe(
            cleanSequence(statement.message ?? ""),
          );
        }
      }
      // Each of Kit's parts can be pointed at.
      const parts =
        kit.type === "flowchart"
          ? [
              ...kit.chart.nodes.flatMap((_, index) =>
                (kit.chart.alias[index] ?? -1) < 0 ? [`node ${index}`] : [],
              ),
              ...kit.chart.groups.map((_, index) => `subgraph ${index}`),
              ...kit.chart.edges.map((_, index) => `edge ${index}`),
            ]
          : [
              ...kit.diagram.participants.map((_, index) => `participant ${index}`),
              ...kit.diagram.events.flatMap((event, index) =>
                event.kind === "message"
                  ? [`message ${index}`]
                  : event.kind === "note"
                    ? [`note ${index}`]
                    : event.kind === "open"
                      ? [`block ${index}`]
                      : [],
              ),
            ];
      expect(parts.filter((part) => !reached.has(part))).toEqual([]);
    });
});
