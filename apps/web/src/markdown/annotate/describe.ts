// Annotations on a drawn Mermaid diagram: the text the model gets for a
// comment on one of its parts, ported from the reference in
// apps/cli/tui/mermaid/annotation.ard and held to the fixtures in
// testdata/mermaid/annotations. Each annotation is a block appended to the
// message: the person's comment, then what Kit read from the diagram, fenced
// as untrusted with a nonce since the diagram's text came from a reply. The
// diagram's source goes in the first block that annotates it; later blocks on
// the same diagram point back to that one.
import {
  arrow,
  type Chart,
  cleanFlow,
  type FlowEdge,
  firstWord,
  type Kind,
  MAX_SOURCE,
  parseFlowchart,
  parseSequence,
  read,
  type SequenceDiagram,
  type SequenceEvent,
  type Shape,
} from "./parse";
import { byteSize, NotDrawn, trimSpace } from "./text";

/** The kinds of part a diagram annotation can point at, as the reference names them. */
export type Target = "node" | "subgraph" | "edge" | "participant" | "message" | "note" | "block";

/** A part of a diagram: its kind, and its index among the reference's nodes, groups, edges, participants or statements. */
export interface Part {
  target: Target;
  index: number;
}

/** One annotation in a message: its comment on a part of a diagram's source, and the nonce its fence is tagged with. */
export interface DiagramAnnotation {
  comment: string;
  source: string;
  part: Part;
  nonce: string;
}

export const OPEN = '<diagram-annotation n="';
export const CLOSE = "</diagram-annotation>";
const FENCE = "UNTRUSTED_DIAGRAM_CONTENT";
/** The most edges, members, or statements listed for one target; the rest are counted. */
const MAX_LISTED = 20;

/** A fresh nonce for an annotation's fence. */
export function nonce(): string {
  return crypto.randomUUID().replaceAll("-", "");
}

// MARK: Describing a part

const quoted = (text: string) => `"${text.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`;

/** id with its label in quotes when the label says something else. */
const named = (id: string, label: string) => (label === "" || label === id ? id : `${id} ${quoted(label)}`);

/** items joined with "; ", the first MAX_LISTED of them, then how many more. */
function listed(items: string[]): string {
  if (items.length === 0) return "none";
  const kept = items.slice(0, MAX_LISTED).join("; ");
  return items.length > MAX_LISTED ? `${kept}; and ${items.length - MAX_LISTED} more` : kept;
}

const SHAPES: Record<Shape, string> = {
  box: "box",
  round: "rounded",
  stadium: "stadium",
  circle: "circle",
  database: "database",
  decision: "decision",
};

/** An edge's link as Mermaid writes it, with its text. */
function link(edge: FlowEdge): string {
  const head = edge.head === "arrow" ? ">" : edge.head === "cross" ? "x" : edge.head === "circle" ? "o" : "";
  const line =
    edge.stroke === "solid"
      ? head === ""
        ? "---"
        : `--${head}`
      : edge.stroke === "dotted"
        ? head === ""
          ? "-.-"
          : `-.-${head}`
        : head === ""
          ? "==="
          : `==${head}`;
  return edge.label === "" ? line : `${line}|${edge.label}|`;
}

const groupName = (chart: Chart, group: number) => {
  const found = chart.groups[group];
  return found ? named(found.id, cleanFlow(found.title)) : "";
};

function endName(chart: Chart, node: number): string {
  const group = chart.alias[node] ?? -1;
  if (group >= 0) return `subgraph ${groupName(chart, group)}`;
  const found = chart.nodes[node];
  return found ? named(found.id, found.text) : "";
}

const edgeText = (chart: Chart, edge: FlowEdge) =>
  `${chart.nodes[edge.from]?.id} ${link(edge)} ${chart.nodes[edge.to]?.id}`;

function inFlowchart(chart: Chart, part: Part): boolean {
  const { index } = part;
  if (index < 0) return false;
  switch (part.target) {
    case "node":
      return index < chart.nodes.length && (chart.alias[index] ?? -1) < 0;
    case "subgraph":
      return index < chart.groups.length;
    case "edge":
      return index < chart.edges.length;
    default:
      return false;
  }
}

function flowchartLines(chart: Chart, part: Part): string[] {
  const { index } = part;
  const lines: string[] = [];
  switch (part.target) {
    case "node": {
      const node = chart.nodes[index];
      if (!node) break;
      lines.push(`Target: node ${named(node.id, node.text)} (${SHAPES[node.shape]})`);
      const group = chart.member[index] ?? -1;
      if (group >= 0) lines.push(`In subgraph: ${groupName(chart, group)}`);
      const into = chart.edges.filter((edge) => edge.to === index).map((edge) => edgeText(chart, edge));
      const out = chart.edges.filter((edge) => edge.from === index).map((edge) => edgeText(chart, edge));
      const neighbors: string[] = [];
      const seen: number[] = [];
      for (const edge of chart.edges)
        if (edge.to === index && edge.from !== index && !seen.includes(edge.from)) {
          seen.push(edge.from);
          neighbors.push(endName(chart, edge.from));
        }
      for (const edge of chart.edges)
        if (edge.from === index && edge.to !== index && !seen.includes(edge.to)) {
          seen.push(edge.to);
          neighbors.push(endName(chart, edge.to));
        }
      lines.push(`Edges in: ${listed(into)}`, `Edges out: ${listed(out)}`, `Neighbors: ${listed(neighbors)}`);
      break;
    }
    case "subgraph": {
      const group = chart.groups[index];
      if (!group) break;
      lines.push(`Target: subgraph ${groupName(chart, index)}`);
      if (group.parent >= 0) lines.push(`In subgraph: ${groupName(chart, group.parent)}`);
      const members: string[] = [];
      chart.nodes.forEach((found, node) => {
        if ((chart.member[node] ?? -1) === index && (chart.alias[node] ?? -1) < 0)
          members.push(named(found.id, found.text));
      });
      chart.groups.forEach((found, inner) => {
        if (found.parent === index) members.push(`subgraph ${groupName(chart, inner)}`);
      });
      lines.push(`Holds: ${listed(members)}`);
      const into: string[] = [];
      const out: string[] = [];
      for (const edge of chart.edges) {
        if ((chart.alias[edge.to] ?? -1) === index) into.push(edgeText(chart, edge));
        if ((chart.alias[edge.from] ?? -1) === index) out.push(edgeText(chart, edge));
      }
      if (into.length > 0) lines.push(`Edges in: ${listed(into)}`);
      if (out.length > 0) lines.push(`Edges out: ${listed(out)}`);
      break;
    }
    case "edge": {
      const edge = chart.edges[index];
      if (!edge) break;
      lines.push(
        `Target: edge ${edgeText(chart, edge)}`,
        `From: ${endName(chart, edge.from)}`,
        `To: ${endName(chart, edge.to)}`,
      );
      break;
    }
  }
  return lines;
}

const participantName = (diagram: SequenceDiagram, index: number) => {
  const found = diagram.participants[index];
  return found ? named(found.id, found.label) : "";
};

const participantId = (diagram: SequenceDiagram, index: number) => diagram.participants[index]?.id ?? "";

/** A message as Mermaid writes it. */
function messageText(diagram: SequenceDiagram, item: SequenceEvent): string {
  const statement = participantId(diagram, item.from) + arrow(item) + participantId(diagram, item.to);
  return item.written === "" ? statement : `${statement}: ${item.written}`;
}

/** A note as Mermaid writes it. */
function noteText(diagram: SequenceDiagram, item: SequenceEvent): string {
  const id = (index: number) => participantId(diagram, index);
  const place =
    item.placement === "left"
      ? `left of ${id(item.from)}`
      : item.placement === "right"
        ? `right of ${id(item.to)}`
        : item.from === item.to
          ? `over ${id(item.from)}`
          : `over ${id(item.from)},${id(item.to)}`;
  return `Note ${place}: ${item.text}`;
}

/** A block's opening or divider statement. */
const blockText = (item: SequenceEvent) => (item.text === "" ? item.keyword : `${item.keyword} ${item.text}`);

/** How many statements of kind come before event index, counting it. */
const ordinal = (diagram: SequenceDiagram, kind: Kind, index: number) =>
  diagram.events.filter((item, at) => at <= index && item.kind === kind).length;

const total = (diagram: SequenceDiagram, kind: Kind) =>
  diagram.events.filter((item) => item.kind === kind).length;

/** The blocks around event index, outermost first, each as its opening statement and, past a divider, the divider's. */
function enclosing(diagram: SequenceDiagram, index: number): string {
  const opens: string[] = [];
  const sections: string[] = [];
  for (const item of diagram.events.slice(0, Math.max(index, 0))) {
    if (item.kind === "open") {
      opens.push(blockText(item));
      sections.push("");
    } else if (item.kind === "divider") {
      if (sections.length > 0) sections[sections.length - 1] = blockText(item);
    } else if (item.kind === "close") {
      if (opens.length > 0) {
        opens.pop();
        sections.pop();
      }
    }
  }
  return opens.map((open, at) => (sections[at] ? `${open} / ${sections[at]}` : open)).join(" > ");
}

function inSequence(diagram: SequenceDiagram, part: Part): boolean {
  const { index } = part;
  if (index < 0) return false;
  const kind = diagram.events[index]?.kind;
  switch (part.target) {
    case "participant":
      return index < diagram.participants.length;
    case "message":
      return kind === "message";
    case "note":
      return kind === "note";
    case "block":
      return kind === "open";
    default:
      return false;
  }
}

function sequenceLines(diagram: SequenceDiagram, part: Part): string[] {
  const { index } = part;
  const lines: string[] = [];
  const inside = () => {
    const around = enclosing(diagram, index);
    if (around !== "") lines.push(`Inside: ${around}`);
  };
  switch (part.target) {
    case "participant": {
      const found = diagram.participants[index];
      if (!found) break;
      lines.push(`Target: ${found.actor ? "actor" : "participant"} ${participantName(diagram, index)}`);
      const sent: string[] = [];
      diagram.events.forEach((item, at) => {
        if (item.kind === "message" && (item.from === index || item.to === index))
          sent.push(`${ordinal(diagram, "message", at)}. ${messageText(diagram, item)}`);
      });
      lines.push(`Messages: ${listed(sent)}`);
      break;
    }
    case "message": {
      const item = diagram.events[index];
      if (!item) break;
      const shown = item.number === "" ? "" : `, numbered ${item.number}`;
      lines.push(
        `Target: message ${ordinal(diagram, "message", index)} of ${total(diagram, "message")}${shown}: ${messageText(diagram, item)}`,
        `From: ${participantName(diagram, item.from)}`,
        `To: ${participantName(diagram, item.to)}`,
      );
      inside();
      break;
    }
    case "note": {
      const item = diagram.events[index];
      if (!item) break;
      lines.push(
        `Target: note ${ordinal(diagram, "note", index)} of ${total(diagram, "note")}: ${noteText(diagram, item)}`,
      );
      inside();
      break;
    }
    case "block": {
      const item = diagram.events[index];
      if (!item) break;
      lines.push(
        `Target: block ${ordinal(diagram, "open", index)} of ${total(diagram, "open")}: ${blockText(item)}`,
      );
      // The statements inside, up to the block's end.
      const dividers: string[] = [];
      const held: string[] = [];
      let depth = 0;
      for (let at = index + 1; at < diagram.events.length; at++) {
        const inner = diagram.events[at];
        if (!inner) break;
        if (inner.kind === "open") depth++;
        else if (inner.kind === "close") {
          if (depth === 0) break;
          depth--;
        } else if (inner.kind === "divider") {
          if (depth === 0) dividers.push(blockText(inner));
        } else if (inner.kind === "message") held.push(`message ${ordinal(diagram, "message", at)}`);
        else if (inner.kind === "note") held.push(`note ${ordinal(diagram, "note", at)}`);
      }
      if (dividers.length > 0) lines.push(`Sections: ${listed(dividers)}`);
      lines.push(`Holds: ${listed(held)}`);
      inside();
      break;
    }
  }
  return lines;
}

/** What Kit reads from source's diagram about a part: its type, title, and the part with what it connects to. */
export function describe(source: string, part: Part): string[] {
  if (byteSize(source) > MAX_SOURCE) throw new NotDrawn(`the source is larger than ${MAX_SOURCE / 1024} KiB`);
  const parts = read(source);
  if (parts.failure !== "") throw new NotDrawn(parts.failure);
  const lines = [`Diagram: ${parts.header}`];
  let known: boolean;
  if (firstWord(parts.header) === "sequenceDiagram") {
    const diagram = parseSequence(parts.body, parts.title);
    known = inSequence(diagram, part);
    if (diagram.title !== "") lines.push(`Title: ${diagram.title}`);
    if (known) lines.push(...sequenceLines(diagram, part));
  } else {
    const chart = parseFlowchart(parts.header, parts.body);
    known = inFlowchart(chart, part);
    const title = cleanFlow(parts.title);
    if (title !== "") lines.push(`Title: ${title}`);
    if (known) lines.push(...flowchartLines(chart, part));
  }
  if (!known) throw new NotDrawn("the diagram has no such part");
  return lines;
}

/** The part, as its description's `Target:` line names it. */
export function summary(source: string, part: Part): string {
  const line = describe(source, part).find((value) => value.startsWith("Target: ")) ?? "";
  return line.slice("Target: ".length);
}

// MARK: The message's text

/** The source as the model gets it: its lines without carriage returns or the blank lines around it. */
const sourceText = (source: string) => source.replaceAll("\r\n", "\n").replace(/^\n+|\n+$/g, "");

/** One annotation's block, numbered number. shared is the number of an earlier block in the message with the same source, or 0. */
export function block(number: number, item: DiagramAnnotation, shared: number): string {
  // A comment's later lines are indented, so none can pass for a line of the block.
  const comment = trimSpace(item.comment).split("\n").join("\n  ");
  const lines = [`${OPEN}${number}">`, `Comment: ${comment}`, `[${FENCE} nonce=${item.nonce}]`];
  lines.push(...describe(item.source, item.part));
  if (shared > 0) lines.push(`Source: as in annotation ${shared}`);
  else lines.push("Source:", sourceText(item.source));
  lines.push(`[/${FENCE} nonce=${item.nonce}]`, CLOSE);
  return lines.join("\n");
}

/** The blocks for a message's annotations, numbered from 1, a blank line apart. */
export function annotationText(items: DiagramAnnotation[]): string {
  const sources: string[] = [];
  return items
    .map((item, offset) => {
      const shared = sources.indexOf(item.source) + 1;
      sources.push(item.source);
      return block(offset + 1, item, shared);
    })
    .join("\n\n");
}

// MARK: Sent annotations

/** A sent annotation, as a transcript shows it. */
export interface SentAnnotation {
  number: number;
  comment: string;
  /** The diagram's type, as its `Diagram:` line names it. */
  diagram: string;
  /** The part, as its `Target:` line names it. */
  target: string;
}

/** The block at the start of text and where it ends: its fence closes with its own nonce, so nothing inside can end it early. */
function parseBlock(text: string): [SentAnnotation, number] | undefined {
  const headEnd = text.indexOf('">\n');
  if (headEnd < 0) return undefined;
  const numberText = text.slice(OPEN.length, headEnd);
  if (!/^\d+$/.test(numberText)) return undefined;
  let at = headEnd + 3;
  if (!text.startsWith("Comment: ", at)) return undefined;
  at += "Comment: ".length;
  const fenceStart = `\n[${FENCE} nonce=`;
  const fence = text.indexOf(fenceStart, at);
  if (fence < 0) return undefined;
  const comment = text.slice(at, fence).replaceAll("\n  ", "\n");
  at = fence + fenceStart.length;
  const nonceEnd = text.indexOf("]\n", at);
  if (nonceEnd < 0) return undefined;
  const value = text.slice(at, nonceEnd);
  if (value === "" || value.includes("\n")) return undefined;
  const body = nonceEnd + 2;
  const closing = `\n[/${FENCE} nonce=${value}]\n${CLOSE}`;
  const end = text.indexOf(closing, body);
  if (end < 0) return undefined;
  const lines = text.slice(body, end).split("\n");
  const field = (key: string) =>
    lines.find((line) => line.startsWith(`${key}: `))?.slice(key.length + 2) ?? "";
  return [
    { number: Number(numberText), comment, diagram: field("Diagram"), target: field("Target") },
    end + closing.length,
  ];
}

/** Splits a message into its own text and the diagram annotation blocks appended to it. Text without blocks comes back unchanged. */
export function splitAnnotations(message: string): { text: string; annotations: SentAnnotation[] } {
  if (!message.includes(OPEN)) return { text: message, annotations: [] };
  let text = "";
  const annotations: SentAnnotation[] = [];
  let rest = message;
  for (let start = rest.indexOf(OPEN); start >= 0; start = rest.indexOf(OPEN)) {
    const found = parseBlock(rest.slice(start));
    if (!found) break;
    text += rest.slice(0, start);
    annotations.push(found[0]);
    rest = rest.slice(start + found[1]);
  }
  text += rest;
  return { text: text.trim(), annotations };
}
