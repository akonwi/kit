// Which part of Kit's reading of a diagram each element Pele drew stands
// for, so a click on the browser's drawing names the same part the other
// clients' hit regions do. Pele tags nodes, edges, clusters, actors,
// messages and notes with a `data-id`; its block frames carry none and are
// drawn in the order their blocks close.
import type { Part } from "./describe";
import type { Diagram } from "./parse";

/** The parts of Pele's parsed model the mapping reads. */
export type PeleModel =
  | { type: "flowchart"; edges: readonly { id?: string; start: string; end: string }[] }
  | { type: "sequence"; messages: readonly { id?: string; type: number; from?: string; to?: string }[] }
  | { type: string };

/** What a drawn element is, as Pele marks it. */
export type Drawn =
  | { kind: "node" | "cluster" | "edge" | "actor" | "message" | "note"; id: string }
  | { kind: "frame"; ordinal: number };

/** Pele's statement types (Mermaid's LINETYPE) that are notes, and that open, divide, or close a block. */
const NOTE = 2;
const NOT_MESSAGES = new Set([
  2, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 26, 27, 28, 29, 30, 31, 32,
]);

const isFlowchart = (model: PeleModel): model is Extract<PeleModel, { type: "flowchart" }> =>
  model.type === "flowchart" && "edges" in model;
const isSequence = (model: PeleModel): model is Extract<PeleModel, { type: "sequence" }> =>
  model.type === "sequence" && "messages" in model;

/** Kit's block statements in the order they close, as Pele draws their frames. */
function blocksByClose(events: readonly { kind: string }[]): number[] {
  const open: number[] = [];
  const closed: number[] = [];
  events.forEach((event, index) => {
    if (event.kind === "open") open.push(index);
    else if (event.kind === "close") {
      const started = open.pop();
      if (started !== undefined) closed.push(started);
    }
  });
  return closed;
}

/** The index of the nth event of kind, or -1. */
function nth(events: readonly { kind: string }[], kind: string, count: number): number {
  let seen = 0;
  for (let index = 0; index < events.length; index++) {
    if (events[index]?.kind !== kind) continue;
    if (seen === count) return index;
    seen++;
  }
  return -1;
}

/** The part of Kit's diagram a drawn element stands for, if any. */
export function partFor(drawn: Drawn, kit: Diagram, pele: PeleModel): Part | undefined {
  if (kit.type === "flowchart") {
    const chart = kit.chart;
    switch (drawn.kind) {
      case "node": {
        const index = chart.nodes.findIndex(
          (node, at) => node.id === drawn.id && (chart.alias[at] ?? -1) < 0,
        );
        if (index >= 0) return { target: "node", index };
        // A node that names a subgraph is drawn as the subgraph.
        const group = chart.groups.findIndex((value) => value.id === drawn.id);
        return group >= 0 ? { target: "subgraph", index: group } : undefined;
      }
      case "cluster": {
        const index = chart.groups.findIndex((group) => group.id === drawn.id);
        return index >= 0 ? { target: "subgraph", index } : undefined;
      }
      case "edge": {
        if (!isFlowchart(pele)) return undefined;
        const at = pele.edges.findIndex((edge) => edge.id === drawn.id);
        const edge = pele.edges[at];
        if (!edge) return undefined;
        // Kit draws a repeated edge once; the same ends are matched in order.
        const same = (start: string, end: string) => start === edge.start && end === edge.end;
        const earlier = pele.edges.slice(0, at).filter((other) => same(other.start, other.end)).length;
        const candidates = chart.edges.flatMap((value, index) =>
          same(chart.nodes[value.from]?.id ?? "", chart.nodes[value.to]?.id ?? "") ? [index] : [],
        );
        const index = candidates[Math.min(earlier, candidates.length - 1)];
        return index === undefined ? undefined : { target: "edge", index };
      }
      default:
        return undefined;
    }
  }
  const diagram = kit.diagram;
  switch (drawn.kind) {
    case "actor": {
      const index = diagram.participants.findIndex((participant) => participant.id === drawn.id);
      return index >= 0 ? { target: "participant", index } : undefined;
    }
    case "message":
    case "note": {
      if (!isSequence(pele) || !drawn.id.startsWith("i")) return undefined;
      const at = pele.messages.findIndex((message) => message.id === drawn.id.slice(1));
      if (at < 0) return undefined;
      const note = (type: number) => type === NOTE;
      const message = (value: { type: number; from?: string; to?: string }) =>
        !NOT_MESSAGES.has(value.type) && value.from !== undefined && value.to !== undefined;
      const statement = pele.messages[at];
      if (!statement) return undefined;
      const kind = note(statement.type) ? "note" : message(statement) ? "message" : undefined;
      if (!kind) return undefined;
      const count = pele.messages
        .slice(0, at)
        .filter((value) => (kind === "note" ? note(value.type) : message(value))).length;
      const index = nth(diagram.events, kind, count);
      return index >= 0 ? { target: kind, index } : undefined;
    }
    case "frame": {
      const index = blocksByClose(diagram.events)[drawn.ordinal];
      return index === undefined ? undefined : { target: "block", index };
    }
    default:
      return undefined;
  }
}

const SELECTOR = [
  ".pele-node[data-id]",
  ".pele-cluster[data-id]",
  ".pele-cluster-title[data-id]",
  ".pele-edge[data-id]",
  ".pele-edge-label[data-id]",
  ".pele-note[data-id]",
  ".pele-frame",
].join(",");

/** Pele's frames for blocks, in the order it drew them. */
const frames = (svg: Element) => [...svg.querySelectorAll(".pele-frame:not(.pele-frame-rect)")];

/** What a drawn element is, from its classes and `data-id`. */
export function drawnOf(element: Element, svg: Element): Drawn | undefined {
  const classes = element.classList;
  if (classes.contains("pele-frame")) {
    const ordinal = frames(svg).indexOf(element);
    return ordinal >= 0 ? { kind: "frame", ordinal } : undefined;
  }
  const id = element.getAttribute("data-id");
  if (id === null) return undefined;
  const sequence = svg.classList.contains("pele-sequence");
  if (classes.contains("pele-note")) return { kind: "note", id };
  if (classes.contains("pele-node")) return { kind: sequence ? "actor" : "node", id };
  if (classes.contains("pele-cluster") || classes.contains("pele-cluster-title"))
    return { kind: "cluster", id };
  if (classes.contains("pele-edge") || classes.contains("pele-edge-label"))
    return { kind: sequence ? "message" : "edge", id };
  return undefined;
}

/** The drawn element under a pointer's target, if it stands for anything. */
export function drawnElement(target: EventTarget | null, svg: Element): Element | undefined {
  if (!(target instanceof Element) || !svg.contains(target)) return undefined;
  return target.closest(SELECTOR) ?? undefined;
}

/** Every drawn element and the part each stands for. */
export function drawnParts(svg: Element, kit: Diagram, pele: PeleModel): { element: Element; part: Part }[] {
  const found: { element: Element; part: Part }[] = [];
  for (const element of svg.querySelectorAll(SELECTOR)) {
    const drawn = drawnOf(element, svg);
    const part = drawn && partFor(drawn, kit, pele);
    if (part) found.push({ element, part });
  }
  return found;
}
