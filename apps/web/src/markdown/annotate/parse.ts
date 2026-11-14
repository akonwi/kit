// Kit's reading of a Mermaid diagram, ported from the reference parser in
// apps/cli/tui/mermaid (flowchart.ard, sequence.ard) by way of the Swift
// port in apps/apple/KitShared/Sources/KitMermaid. The browser draws with
// Pele, but describes a part to the model the way every other client does,
// so it reads the source as Kit reads it. Only parsing is ported, not layout.
import {
  atoi,
  byteSize,
  cut,
  fields,
  indexAny,
  NotDrawn,
  toLower,
  toUpper,
  trim,
  trimPrefix,
  trimSpace,
  trimSuffix,
} from "./text";

/** The largest source read, in UTF-8 bytes. */
export const MAX_SOURCE = 16384;

export interface Parts {
  title: string;
  header: string;
  body: string[];
  failure: string;
}

export function firstWord(line: string): string {
  const space = indexAny(line, " \t");
  return space >= 0 ? line.slice(0, space) : line;
}

/**
 * A source read into the parts every diagram type shares: the frontmatter's
 * title, the header statement that names the diagram's type, the lines after
 * it, and why it can't be read, or empty.
 */
export function read(source: string): Parts {
  const lines = source.replaceAll("\r\n", "\n").split("\n");
  const line = (index: number) => lines[index] ?? "";
  let index = 0;
  const parts: Parts = { title: "", header: "", body: [], failure: "" };
  while (index < lines.length && trimSpace(line(index)) === "") index++;
  // YAML frontmatter, of which only the title is used.
  if (trimSpace(line(index)) === "---") {
    index++;
    let closed = false;
    while (!closed && index < lines.length) {
      const trimmed = trimSpace(line(index));
      if (trimmed === "---") closed = true;
      else if (line(index).startsWith("title:"))
        parts.title = trim(trimSpace(trimPrefix(trimmed, "title:")), "\"'");
      index++;
    }
    if (!closed) parts.failure = "the frontmatter isn't closed";
  }
  // The header is the first statement; comments and directives come first.
  while (parts.header === "" && index < lines.length) {
    const trimmed = trimSpace(line(index));
    if (trimmed !== "" && !trimmed.startsWith("%%")) parts.header = trimmed;
    index++;
  }
  parts.body = lines.slice(index);
  return parts;
}

// MARK: Flowcharts

export type Shape = "box" | "round" | "stadium" | "circle" | "database" | "decision";
export type Stroke = "solid" | "dotted" | "thick";
export type Head = "arrow" | "cross" | "circle" | "none";

export interface FlowNode {
  id: string;
  text: string;
  shape: Shape;
}

export interface FlowEdge {
  from: number;
  to: number;
  stroke: Stroke;
  head: Head;
  label: string;
}

export interface Group {
  id: string;
  title: string;
  /** The subgraph it sits in, or -1 at the top level. */
  parent: number;
}

export interface Chart {
  nodes: FlowNode[];
  edges: FlowEdge[];
  groups: Group[];
  /** Each node's subgraph, or -1 at the top level. */
  member: number[];
  /** The subgraph a node's id names, or -1. Such a node stands for the subgraph. */
  alias: number[];
}

const FLOW_MAX_NODES = 60;
const FLOW_MAX_EDGES = 120;
const FLOW_MAX_STATEMENTS = 200;
const FLOW_MAX_GROUPS = 20;
const FLOW_MAX_TEXT = 40;

/** Node, link, and subgraph text as a flowchart shows it. */
export function cleanFlow(value: string): string {
  return cut(value, FLOW_MAX_TEXT);
}

/** Link text as one line, without the quotes it may be written in. */
function labelText(value: string): string {
  const text = trimSpace(value);
  const quoted = text.length >= 2 && text.startsWith('"') && text.endsWith('"');
  return quoted ? cleanFlow(text.slice(1, -1)) : cleanFlow(text);
}

const sameEdge = (a: FlowEdge, b: FlowEdge) =>
  a.from === b.from && a.to === b.to && a.stroke === b.stroke && a.head === b.head && a.label === b.label;

/** A line of source, read by index as the reference reads its bytes; every character it looks for is ASCII. */
class Line {
  constructor(readonly text: string) {}

  get size() {
    return this.text.length;
  }

  is(index: number, char: string): boolean {
    return index >= 0 && this.text[index] === char;
  }

  isAny(index: number, chars: string): boolean {
    const char = this.text[index];
    return char !== undefined && char.charCodeAt(0) < 0x80 && chars.includes(char);
  }

  slice(start: number, end: number = this.text.length): string | undefined {
    if (start < 0 || start > end || end > this.text.length) return undefined;
    return this.text.slice(start, end);
  }

  /** Whether a link starts at index. */
  linkStarts(index: number): boolean {
    const current = this.text[index];
    const next = this.text[index + 1];
    return (
      (current === "-" && (next === "-" || next === ".")) ||
      (current === "=" && next === "=") ||
      (current === "~" && next === "~") ||
      (current === "<" && (next === "-" || next === "="))
    );
  }

  skipSpace(index: number): number {
    let at = index;
    while (at < this.size && (this.is(at, " ") || this.is(at, "\t"))) at++;
    return at;
  }

  /** The end of the name that starts at index. */
  nameEnd(index: number): number {
    let at = index;
    while (at < this.size && !this.isAny(at, ' \t[](){}<>&;|":') && !this.linkStarts(at)) at++;
    return at;
  }

  runOf(index: number, chars: string): number {
    let at = index;
    while (this.isAny(at, chars)) at++;
    return at;
  }

  /** The end of the link token starting at index: its line, then a head. */
  linkEnd(index: number): number {
    const end = this.runOf(index, "-=.~<");
    return this.isAny(end, ">xo") ? end + 1 : end;
  }

  /** Whether a link starts anywhere outside quoted text. */
  hasLink(): boolean {
    let found = false;
    let quoted = false;
    for (let index = 0; index < this.size; index++) {
      if (this.is(index, '"')) quoted = !quoted;
      if (!quoted && this.linkStarts(index)) found = true;
    }
    return found;
  }
}

interface Link {
  stroke: Stroke;
  head: Head;
  label: string;
}

interface Scan {
  next: number;
  nodes: number[];
  link: Link;
}

const noLink = (): Link => ({ stroke: "solid", head: "none", label: "" });

/** The stroke and head of a whole link token such as `-->`, `-.-`, or `==>`. */
function classify(token: string): Link {
  const last = token.slice(-1);
  const head: Head = last === ">" ? "arrow" : last === "x" ? "cross" : last === "o" ? "circle" : "none";
  const line = head === "none" ? token : token.slice(0, -1);
  const headed = head !== "none";
  const dashes = line.length >= 2 && trim(line, "-") === "";
  const equals = line.length >= 2 && trim(line, "=") === "";
  const inner = line.length >= 2 ? line.slice(1, -1) : "";
  const dotted =
    line.length >= 3 && line.startsWith("-") && line.endsWith("-") && inner !== "" && trim(inner, ".") === "";
  if (dashes && (headed || line.length >= 3)) return { stroke: "solid", head, label: "" };
  if (equals && (headed || line.length >= 3)) return { stroke: "thick", head, label: "" };
  if (dotted) return { stroke: "dotted", head, label: "" };
  throw new NotDrawn(`\`${token}\` links aren't drawn yet`);
}

const FLOWS = new Set(["TD", "TB", "BT", "LR", "RL"]);

class FlowParser {
  nodes: FlowNode[] = [];
  indexes = new Map<string, number>();
  edges: FlowEdge[] = [];
  statements = 0;
  groups: Group[] = [];
  open: number[] = [];
  member: number[] = [];
  mentions: number[][] = [];
  explicit: boolean[] = [];

  node(id: string, text: string | undefined, shape: Shape): number {
    let index = this.indexes.get(id);
    if (index === undefined) {
      index = this.nodes.length;
      this.nodes.push({ id, text: cleanFlow(id), shape: "box" });
      this.indexes.set(id, index);
      this.member.push(-1);
      this.explicit.push(false);
    }
    const current = this.open.at(-1) ?? -1;
    if (current >= 0) this.mentions[current]?.push(index);
    if (text !== undefined) {
      this.explicit[index] = true;
      this.nodes[index] = { id, text: cleanFlow(text), shape };
    }
    return index;
  }

  /** One node: an id, then optionally a shape with text and a class suffix. */
  one(line: Line, start: number): Scan {
    const end = line.nameEnd(start);
    const id = line.slice(start, end) ?? "";
    let at = end;
    let text: string | undefined;
    let shape: Shape = "box";
    let failure = "";
    const rest = line.slice(at) ?? "";
    let opened: string | undefined;
    if (rest.startsWith("(((")) opened = undefined;
    else if (rest.startsWith("((")) opened = "((";
    else if (rest.startsWith("([")) opened = "([";
    else if (rest.startsWith("(")) opened = "(";
    else if (rest.startsWith("[(")) opened = "[(";
    else if (rest.startsWith("[[") || rest.startsWith("[/") || rest.startsWith("[\\")) opened = undefined;
    else if (rest.startsWith("[")) opened = "[";
    else if (rest.startsWith("{{")) opened = undefined;
    else if (rest.startsWith("{")) opened = "{";
    const shaped = line.isAny(at, "[({>");
    if (id === "") failure = `unsupported line \`${line.text}\``;
    else if (shaped && opened === undefined) failure = `\`${rest.slice(0, 2)}\` nodes aren't drawn yet`;
    else if (opened !== undefined) {
      const [close, kind]: [string, Shape] =
        opened === "(("
          ? ["))", "circle"]
          : opened === "(["
            ? ["])", "stadium"]
            : opened === "("
              ? [")", "round"]
              : opened === "[("
                ? [")]", "database"]
                : opened === "{"
                  ? ["}", "decision"]
                  : ["]", "box"];
      shape = kind;
      const content = at + opened.length;
      const quoted = line.is(content, '"');
      let closing: number;
      if (quoted) {
        const quote = (line.slice(content + 1) ?? "").indexOf('"');
        if (quote < 0) closing = -1;
        else {
          const after = content + 1 + quote + 1;
          closing = (line.slice(after) ?? "").startsWith(close) ? after : -1;
        }
      } else {
        const found = (line.slice(content) ?? "").indexOf(close);
        closing = found < 0 ? -1 : content + found;
      }
      if (closing < 0) failure = `unclosed \`${opened}\` in \`${line.text}\``;
      else {
        let value = line.slice(content, closing) ?? "";
        if (quoted) value = value.slice(1, -1);
        text = value;
        at = closing + close.length;
      }
    }
    if (failure === "" && (line.slice(at, at + 3) ?? "") === ":::") at = line.nameEnd(at + 3);
    if (failure !== "") throw new NotDrawn(failure);
    return { next: at, nodes: [this.node(id, text, shape)], link: noLink() };
  }

  /** Nodes joined by `&`. */
  group(line: Line, start: number): Scan {
    const found: number[] = [];
    let at = line.skipSpace(start);
    let more = true;
    while (more) {
      const scan = this.one(line, at);
      found.push(...scan.nodes);
      at = line.skipSpace(scan.next);
      more = line.is(at, "&");
      if (more) at = line.skipSpace(at + 1);
      if (this.nodes.length > FLOW_MAX_NODES + FLOW_MAX_GROUPS)
        throw new NotDrawn(`more than ${FLOW_MAX_NODES} nodes`);
    }
    return { next: at, nodes: found, link: noLink() };
  }

  /** A link between groups, with its text when it has one. */
  link(line: Line, start: number): Scan {
    const runEnd = line.runOf(start, "-=.");
    const run = line.slice(start, runEnd) ?? "";
    const opening = run === "--" || run === "-." || run === "==";
    if (opening && line.is(runEnd, " ")) {
      // `-- text -->`: the text runs up to the closing part of the link.
      const closings =
        run === "-." ? [".->", ".-"] : run === "==" ? ["==>", "==="] : ["-->", "---", "--x", "--o"];
      const rest = line.slice(runEnd) ?? "";
      let found = -1;
      for (const closing of closings) {
        const at = rest.indexOf(closing);
        if (at >= 0 && (found < 0 || at < found)) found = at;
      }
      if (found < 0) throw new NotDrawn(`unsupported link in \`${line.text}\``);
      const label = trimSpace(rest.slice(0, found));
      const closeStart = runEnd + found;
      const closeEnd = line.linkEnd(closeStart);
      const link = classify(run + (line.slice(closeStart, closeEnd) ?? ""));
      link.label = labelText(label);
      return { next: closeEnd, nodes: [], link };
    }
    const end = line.linkEnd(start);
    const link = classify(line.slice(start, end) ?? "");
    // `-->|text|`, or `--> |text|`
    const bar = line.skipSpace(end);
    if (!line.is(bar, "|")) return { next: end, nodes: [], link };
    const close = (line.slice(bar + 1) ?? "").indexOf("|");
    if (close < 0) throw new NotDrawn(`unclosed \`|\` in \`${line.text}\``);
    link.label = labelText(line.slice(bar + 1, bar + 1 + close) ?? "");
    return { next: bar + 1 + close + 1, nodes: [], link };
  }

  /** A statement: groups joined by links. Returns where it ended. */
  statement(line: Line, start: number): number {
    this.statements++;
    if (this.statements > FLOW_MAX_STATEMENTS)
      throw new NotDrawn(`more than ${FLOW_MAX_STATEMENTS} statements`);
    const first = this.group(line, start);
    let previous = first.nodes;
    let at = line.skipSpace(first.next);
    while (at < line.size && !line.is(at, ";")) {
      if (!line.linkStarts(at)) throw new NotDrawn(`unsupported line \`${line.text}\``);
      const link = this.link(line, at);
      const next = this.group(line, link.next);
      if (this.edges.length + previous.length * next.nodes.length > FLOW_MAX_EDGES)
        throw new NotDrawn(`more than ${FLOW_MAX_EDGES} edges`);
      for (const from of previous)
        for (const to of next.nodes) {
          const edge = { from, to, ...link.link };
          if (!this.edges.some((other) => sameEdge(other, edge))) this.edges.push(edge);
        }
      previous = next.nodes;
      at = line.skipSpace(next.next);
    }
    return at + 1;
  }

  line(text: string) {
    const line = new Line(text);
    const word = firstWord(text);
    switch (word) {
      case "subgraph":
        this.subgraph(trimSpace(text.slice(word.length)));
        return;
      case "end":
      case "end;": {
        const closing = this.open.at(-1) ?? -1;
        if (text !== "end" && text !== "end;") throw new NotDrawn("unexpected text after `end`");
        if (closing < 0) throw new NotDrawn("`end` without a `subgraph`");
        for (const node of this.mentions[closing] ?? [])
          if ((this.member[node] ?? -1) < 0) this.member[node] = closing;
        this.open.pop();
        return;
      }
      case "direction": {
        const direction = toUpper(trimSuffix(trimSpace(text.slice(word.length)), ";"));
        if ((this.open.at(-1) ?? -1) < 0) throw new NotDrawn("`direction` is only drawn inside a subgraph");
        if (!FLOWS.has(direction)) throw new NotDrawn(`unknown direction \`${direction}\``);
        return;
      }
      case "classDef":
      case "class":
      case "style":
      case "linkStyle":
      case "click":
        if (line.hasLink()) throw new NotDrawn(`unsupported line \`${text}\``);
        return;
      default: {
        let at = 0;
        while (at < line.size) {
          at = line.skipSpace(at);
          if (at < line.size) at = this.statement(line, at);
        }
      }
    }
  }

  /** Opens a subgraph: `subgraph id`, `subgraph id[title]`, or `subgraph "title"`. */
  subgraph(rest: string) {
    const quoted = rest.length >= 2 && rest.startsWith('"') && rest.endsWith('"');
    const bracket = rest.indexOf("[");
    let id = rest;
    let title = rest;
    let failure = "";
    if (quoted) {
      id = labelText(rest);
      title = id;
    } else if (bracket >= 0) {
      if (rest.endsWith("]")) {
        id = trimSpace(rest.slice(0, bracket));
        title = labelText(rest.slice(bracket + 1, -1));
      } else failure = `unclosed \`[\` in \`subgraph ${rest}\``;
    }
    for (const group of this.groups) if (group.id === id) failure = `subgraph \`${id}\` is declared twice`;
    if (failure !== "") throw new NotDrawn(failure);
    if (id === "") throw new NotDrawn("a subgraph needs a name");
    if (this.groups.length >= FLOW_MAX_GROUPS) throw new NotDrawn(`more than ${FLOW_MAX_GROUPS} subgraphs`);
    this.open.push(this.groups.length);
    this.mentions.push([]);
    this.groups.push({ id, title: cleanFlow(title), parent: this.open.at(-2) ?? -1 });
  }
}

/** Parses the statements after a flowchart header. */
export function parseFlowchart(header: string, lines: string[]): Chart {
  const words = fields(header);
  const direction = toUpper(trimSuffix(words[1] ?? "TD", ";"));
  const parser = new FlowParser();
  if (words.length > 2) throw new NotDrawn(`unexpected text after \`${words[0] ?? ""} ${direction}\``);
  if (!FLOWS.has(direction)) throw new NotDrawn(`unknown direction \`${direction}\``);
  for (const raw of lines) {
    const line = trimSpace(raw);
    if (line !== "" && !line.startsWith("%%")) parser.line(line);
  }
  const open = parser.open.at(-1);
  if (open !== undefined)
    throw new NotDrawn(`subgraph \`${parser.groups[open]?.id}\` isn't closed with \`end\``);
  // A node whose id names a subgraph stands for it.
  const alias: number[] = [];
  let aliases = 0;
  parser.nodes.forEach((node, index) => {
    let named = -1;
    parser.groups.forEach((group, number) => {
      if (group.id === node.id) named = number;
    });
    if (named >= 0 && parser.explicit[index])
      throw new NotDrawn(`\`${node.id}\` names both a node and a subgraph`);
    if (named >= 0) aliases++;
    alias.push(named);
  });
  if (parser.nodes.length === 0) throw new NotDrawn("the flowchart has no nodes");
  if (parser.nodes.length - aliases > FLOW_MAX_NODES) throw new NotDrawn(`more than ${FLOW_MAX_NODES} nodes`);
  if (parser.edges.length > FLOW_MAX_EDGES) throw new NotDrawn(`more than ${FLOW_MAX_EDGES} edges`);
  return { nodes: parser.nodes, edges: parser.edges, groups: parser.groups, member: parser.member, alias };
}

// MARK: Sequence diagrams

export interface Participant {
  id: string;
  label: string;
  actor: boolean;
}

export type Kind = "message" | "note" | "open" | "divider" | "close";
export type Tip = "none" | "filled" | "open" | "cross";
export type Placement = "left" | "right" | "over";

export interface SequenceEvent {
  kind: Kind;
  from: number;
  to: number;
  dashed: boolean;
  start: Tip;
  end: Tip;
  placement: Placement;
  keyword: string;
  text: string;
  /** A message's text as written, before any number, and the number `autonumber` gives it. */
  written: string;
  number: string;
}

export interface SequenceDiagram {
  title: string;
  participants: Participant[];
  events: SequenceEvent[];
}

const SEQ_MAX_PARTICIPANTS = 24;
const SEQ_MAX_STATEMENTS = 200;
const SEQ_MAX_TEXT = 64;

export function cleanSequence(value: string): string {
  return cut(value, SEQ_MAX_TEXT);
}

const event = (kind: Kind, values: Partial<SequenceEvent> = {}): SequenceEvent => ({
  kind,
  from: 0,
  to: 0,
  dashed: false,
  start: "none",
  end: "none",
  placement: "over",
  keyword: "",
  text: "",
  written: "",
  number: "",
  ...values,
});

interface Arrow {
  token: string;
  dashed: boolean;
  start: Tip;
  end: Tip;
}

const ARROWS: Arrow[] = [
  { token: "<<-->>", dashed: true, start: "filled", end: "filled" },
  { token: "<<->>", dashed: false, start: "filled", end: "filled" },
  { token: "-->>", dashed: true, start: "none", end: "filled" },
  { token: "->>", dashed: false, start: "none", end: "filled" },
  { token: "--x", dashed: true, start: "none", end: "cross" },
  { token: "-x", dashed: false, start: "none", end: "cross" },
  { token: "--)", dashed: true, start: "none", end: "open" },
  { token: "-)", dashed: false, start: "none", end: "open" },
  { token: "-->", dashed: true, start: "none", end: "none" },
  { token: "->", dashed: false, start: "none", end: "none" },
];

/** A message's arrow as Mermaid writes it. */
export function arrow(item: SequenceEvent): string {
  return (
    ARROWS.findLast(
      (value) => value.dashed === item.dashed && value.start === item.start && value.end === item.end,
    )?.token ?? "->"
  );
}

function validName(id: string): boolean {
  if (id === "" || indexAny(id, ":,;<>") >= 0) return false;
  return !["->", "--", "-x", "-)"].some((piece) => id.includes(piece));
}

const article = (word: string) => (word.startsWith("a") || word.startsWith("o") ? "an" : "a");

class SequenceParser {
  participants: Participant[] = [];
  indexes = new Map<string, number>();
  events: SequenceEvent[] = [];
  blocks: string[] = [];
  numbered = false;
  number = 1n;
  step = 1n;

  constructor(public title: string) {}

  participant(raw: string): number {
    const id = trimSpace(raw);
    if (id === "") throw new NotDrawn("a participant needs a name");
    if (!validName(id)) throw new NotDrawn(`\`${id}\` isn't a participant name`);
    const found = this.indexes.get(id);
    if (found !== undefined) return found;
    const index = this.participants.length;
    this.participants.push({ id, label: cleanSequence(id), actor: false });
    this.indexes.set(id, index);
    return index;
  }

  declare(rest: string, actor: boolean) {
    const at = rest.indexOf(" as ");
    const id = at >= 0 ? rest.slice(0, at) : rest;
    const label = at >= 0 ? cleanSequence(rest.slice(at + 4)) : cleanSequence(id);
    if (rest.includes("@{")) throw new NotDrawn("participant metadata isn't supported");
    if (rest.endsWith(" as") || label === "") throw new NotDrawn(`\`${rest}\` needs a label after \`as\``);
    const index = this.participant(id);
    this.participants[index] = { id: trimSpace(id), label, actor };
  }

  note(rest: string) {
    const colon = rest.indexOf(":");
    const head = trimSpace(colon >= 0 ? rest.slice(0, colon) : rest);
    const text = colon >= 0 ? cleanSequence(rest.slice(colon + 1)) : "";
    const lower = toLower(head);
    let placement: Placement;
    let names: string;
    if (lower.startsWith("left of ")) {
      placement = "left";
      names = head.slice(8);
    } else if (lower.startsWith("right of ")) {
      placement = "right";
      names = head.slice(9);
    } else if (lower.startsWith("over ")) {
      placement = "over";
      names = head.slice(5);
    } else throw new NotDrawn(`unsupported note placement \`${head}\``);
    const targets = placement === "over" ? names.split(",") : [names];
    if (targets.length > 2) throw new NotDrawn("a note spans at most two participants");
    const first = this.participant(targets[0] ?? "");
    const second = this.participant(targets[targets.length - 1] ?? "");
    this.events.push(
      event("note", { from: Math.min(first, second), to: Math.max(first, second), placement, text }),
    );
  }

  message(line: string) {
    // The earliest arrow separates sender from receiver.
    let found: Arrow | undefined;
    let foundAt = -1;
    for (const candidate of ARROWS) {
      const at = line.indexOf(candidate.token);
      if (at > 0 && (foundAt < 0 || at < foundAt)) {
        found = candidate;
        foundAt = at;
      }
    }
    if (!found) throw new NotDrawn(`unsupported line \`${line}\``);
    const sender = line.slice(0, foundAt);
    const rest = line.slice(foundAt + found.token.length);
    const colon = rest.indexOf(":");
    let receiver = trimSpace(colon >= 0 ? rest.slice(0, colon) : rest);
    if (receiver.startsWith("+") || receiver.startsWith("-")) receiver = receiver.slice(1);
    let text = colon >= 0 ? cleanSequence(rest.slice(colon + 1)) : "";
    const from = this.participant(sender);
    const to = this.participant(receiver);
    const written = text;
    let shown = "";
    if (this.numbered) {
      shown = `${this.number}`;
      text = cleanSequence(`${this.number}. ${text}`);
      // Numbers wrap as the reference's Go integers do.
      this.number = BigInt.asIntN(64, this.number + this.step);
    }
    this.events.push(
      event("message", {
        from,
        to,
        dashed: found.dashed,
        start: found.start,
        end: found.end,
        text,
        written,
        number: shown,
      }),
    );
  }

  divider(keyword: string, rest: string, within: string) {
    if ((this.blocks.at(-1) ?? "") !== within)
      throw new NotDrawn(`\`${keyword}\` outside ${article(within)} \`${within}\` block`);
    this.events.push(event("divider", { keyword, text: cleanSequence(rest) }));
  }

  autonumber(rest: string) {
    const words = fields(rest);
    if (rest === "off") {
      this.numbered = false;
      return;
    }
    if (words.length > 2) throw new NotDrawn("`autonumber` takes at most a start and a step");
    const start = words[0] === undefined ? 1n : atoi(words[0]);
    const by = words[1] === undefined ? 1n : atoi(words[1]);
    if (start === undefined || by === undefined) throw new NotDrawn("`autonumber` takes whole numbers");
    this.numbered = true;
    this.number = start;
    this.step = by;
  }

  statement(line: string) {
    const space = indexAny(line, " \t");
    const original = space >= 0 ? line.slice(0, space) : line;
    // Keywords match in any case, as Mermaid's do.
    const word = toLower(original);
    const rest = space >= 0 ? trimSpace(line.slice(space + 1)) : "";
    switch (word) {
      case "participant":
        return this.declare(rest, false);
      case "actor":
        return this.declare(rest, true);
      case "note":
        return this.note(rest);
      case "loop":
      case "alt":
      case "opt":
      case "par":
      case "critical":
      case "break":
        this.blocks.push(word);
        this.events.push(event("open", { keyword: word, text: cleanSequence(rest) }));
        return;
      case "rect":
        this.blocks.push("rect");
        return;
      case "else":
        return this.divider(word, rest, "alt");
      case "and":
        return this.divider(word, rest, "par");
      case "option":
        return this.divider(word, rest, "critical");
      case "end": {
        if (rest !== "") throw new NotDrawn("unexpected text after `end`");
        const top = this.blocks.pop();
        if (top === undefined) throw new NotDrawn("`end` without an open block");
        if (top !== "rect") this.events.push(event("close"));
        return;
      }
      case "activate":
      case "deactivate":
        this.participant(rest);
        return;
      case "autonumber":
        return this.autonumber(rest);
      case "box":
      case "create":
      case "destroy":
      case "link":
      case "links":
      case "properties":
      case "details":
        throw new NotDrawn(`\`${word}\` isn't supported`);
      default:
        if (word === "title" || word.startsWith("title:"))
          this.title = cleanSequence(trimPrefix(line.slice(5), ":"));
        else this.message(line);
    }
  }
}

/** Parses the statements after a `sequenceDiagram` header; a `title` statement replaces the frontmatter's. */
export function parseSequence(lines: string[], title: string): SequenceDiagram {
  const parser = new SequenceParser(cleanSequence(title));
  let statements = 0;
  for (const raw of lines) {
    const line = trimSpace(raw);
    if (line !== "" && !line.startsWith("%%")) {
      statements++;
      parser.statement(line);
    }
  }
  const unclosed = parser.blocks.at(-1);
  if (unclosed !== undefined) throw new NotDrawn(`unclosed \`${unclosed}\` block`);
  if (parser.participants.length === 0) throw new NotDrawn("the diagram has no participants");
  if (parser.participants.length > SEQ_MAX_PARTICIPANTS)
    throw new NotDrawn(`more than ${SEQ_MAX_PARTICIPANTS} participants`);
  if (statements > SEQ_MAX_STATEMENTS) throw new NotDrawn(`more than ${SEQ_MAX_STATEMENTS} statements`);
  return { title: parser.title, participants: parser.participants, events: parser.events };
}

/** A diagram as Kit reads it, or why it can't be. */
export type Diagram =
  | { type: "flowchart"; header: string; title: string; chart: Chart }
  | { type: "sequence"; header: string; title: string; diagram: SequenceDiagram };

export function readDiagram(source: string): Diagram {
  if (byteSize(source) > MAX_SOURCE) throw new NotDrawn(`the source is larger than ${MAX_SOURCE / 1024} KiB`);
  const parts = read(source);
  if (parts.failure !== "") throw new NotDrawn(parts.failure);
  if (firstWord(parts.header) === "sequenceDiagram")
    return {
      type: "sequence",
      header: parts.header,
      title: parts.title,
      diagram: parseSequence(parts.body, parts.title),
    };
  return {
    type: "flowchart",
    header: parts.header,
    title: parts.title,
    chart: parseFlowchart(parts.header, parts.body),
  };
}
