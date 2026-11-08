// Mermaid diagrams drawn by Pele, made safe for the page: its SVG is parsed
// apart from the document, kept to drawing elements, and given its inline
// styles through the CSSOM, which the page's style policy allows where a
// style attribute in markup is refused.

type Pele = typeof import("./pele");

let loading: Promise<Pele> | undefined;

/** Pele, loaded once; a failed load is tried again next time. */
export function loadPele(): Promise<Pele> {
  loading ??= import("./pele").catch((error: unknown) => {
    loading = undefined;
    throw error;
  });
  return loading;
}

/**
 * The limits Kit's own renderer holds a diagram to (apps/cli/tui/mermaid),
 * so the browser refuses what the other clients refuse for size.
 */
export const LIMITS = {
  sourceBytes: 16 * 1024,
  statements: 200,
  nodes: 60,
  edges: 120,
  subgraphs: 20,
  participants: 24,
} as const;

/** Why a diagram isn't drawn, decided by Kit rather than by Pele. */
export class DiagramRefused extends Error {}

/**
 * How Kit has Pele draw: nothing reaching outside the page, and participants
 * named once, at the top, as the Mac draws a sequence. Pele's own limits sit
 * just past Kit's, so it stops parsing where Kit would refuse anyway.
 */
export function renderOptions(fontFamily: string) {
  return {
    fontFamily,
    limit: LIMITS.sourceBytes,
    maxEdges: LIMITS.edges + 1,
    links: false,
    images: false,
    config: { sequence: { mirrorActors: false } },
  };
}

/** The parts of Pele's models the limits count. */
type Counted =
  | {
      type: "flowchart";
      nodes: ReadonlyMap<string, unknown>;
      edges: readonly unknown[];
      subgraphs: readonly unknown[];
    }
  | {
      type: "sequence";
      actors: ReadonlyMap<string, unknown>;
      messages: readonly unknown[];
      notes: readonly unknown[];
    }
  | { type: string };

/** Which of Kit's limits a parsed diagram is over, in Kit's words. */
export function overLimit(model: Counted): string | undefined {
  if (model.type === "flowchart" && "nodes" in model) {
    if (model.nodes.size > LIMITS.nodes) return `more than ${LIMITS.nodes} nodes`;
    if (model.edges.length > LIMITS.edges) return `more than ${LIMITS.edges} edges`;
    if (model.subgraphs.length > LIMITS.subgraphs) return `more than ${LIMITS.subgraphs} subgraphs`;
  }
  if (model.type === "sequence" && "actors" in model) {
    if (model.actors.size > LIMITS.participants) return `more than ${LIMITS.participants} participants`;
    if (model.messages.length + model.notes.length > LIMITS.statements)
      return `more than ${LIMITS.statements} statements`;
  }
  return undefined;
}

/**
 * Draws a diagram within Kit's limits: its size before Pele reads it, then
 * what it holds before Pele lays it out.
 */
export function drawDiagram(
  pele: Pick<Pele, "parse" | "render">,
  source: string,
  options: ReturnType<typeof renderOptions> & { maxWidth?: number; idPrefix?: string },
) {
  if (new TextEncoder().encode(source).byteLength > LIMITS.sourceBytes)
    throw new DiagramRefused("the source is larger than 16 KiB");
  const model = pele.parse(source, options);
  const reason = overLimit(model as Counted);
  if (reason) throw new DiagramRefused(reason);
  return pele.render(model, options);
}

/** The SVG elements a diagram may use; anything else is dropped with its content. */
const DRAWING = new Set([
  "svg",
  "g",
  "defs",
  "marker",
  "clipPath",
  "path",
  "rect",
  "circle",
  "ellipse",
  "line",
  "polyline",
  "polygon",
  "text",
  "tspan",
  "title",
  "desc",
  "linearGradient",
  "radialGradient",
  "stop",
  "pattern",
  "mask",
]);

/**
 * A `style` attribute's declarations as name and value pairs, split at the
 * semicolons and colons that aren't inside parentheses or quotes.
 */
export function styleDeclarations(text: string): [string, string][] {
  const parts: string[] = [];
  let depth = 0;
  let quote = "";
  let start = 0;
  for (let index = 0; index < text.length; index++) {
    const char = text[index];
    if (quote) {
      if (char === "\\") index++;
      else if (char === quote) quote = "";
    } else if (char === '"' || char === "'") quote = char;
    else if (char === "(") depth++;
    else if (char === ")") depth = Math.max(0, depth - 1);
    else if (char === ";" && depth === 0) {
      parts.push(text.slice(start, index));
      start = index + 1;
    }
  }
  parts.push(text.slice(start));
  const declarations: [string, string][] = [];
  for (const part of parts) {
    const colon = part.indexOf(":");
    if (colon < 0) continue;
    const name = part.slice(0, colon).trim();
    const value = part.slice(colon + 1).trim();
    if (name && value) declarations.push([name, value]);
  }
  return declarations;
}

/**
 * Whether a value refers only within the drawing: any `url()` names one of
 * its own ids, and nothing else asks for a resource.
 */
export function refersWithin(value: string): boolean {
  if (/^\s*(javascript|data|vbscript):/i.test(value)) return false;
  if (/\b(image|image-set|cross-fade|element|src)\s*\(/i.test(value)) return false;
  return [...value.matchAll(/url\(\s*(['"]?)\s*([^'")\s]*)/gi)].every((match) => match[2]?.startsWith("#"));
}

/** Whether an attribute can stay on a drawing: no handlers, and links only within it. */
export function keepAttribute(name: string, value: string): boolean {
  const lower = name.toLowerCase();
  if (lower.startsWith("on") || lower === "xml:base") return false;
  if (lower === "href" || lower === "xlink:href") return value.startsWith("#");
  return refersWithin(value);
}

/** Where a drawing's inline styles wait while it's parsed apart from the page. */
const STYLE = "data-pele-style";

/**
 * The SVG with its `style` attributes renamed, so parsing it doesn't apply
 * them, which the page's policy reports as a violation even in a document
 * that's never shown. It reads tags and quoted values the way XML does: text
 * can't hold an unescaped `<`, and a quoted value may hold `>`.
 */
export function withoutInlineStyles(svg: string): string {
  let out = "";
  let index = 0;
  while (index < svg.length) {
    const open = svg.indexOf("<", index);
    if (open < 0) break;
    out += svg.slice(index, open + 1);
    index = open + 1;
    let quote = "";
    while (index < svg.length) {
      const char = svg[index] ?? "";
      if (quote) {
        if (char === quote) quote = "";
        out += char;
        index++;
      } else if (char === '"' || char === "'") {
        quote = char;
        out += char;
        index++;
      } else if (char === ">") {
        break;
      } else if (/\s/.test(char) && /^\s+style\s*=/.test(svg.slice(index, index + 64))) {
        const name = /^\s+style/.exec(svg.slice(index))?.[0] ?? "";
        out += `${name.slice(0, -"style".length)}${STYLE}`;
        index += name.length;
      } else {
        out += char;
        index++;
      }
    }
  }
  return out + svg.slice(index);
}

/**
 * The SVG Pele drew, as an element of this document. Its styles are applied
 * through the CSSOM, and anything that isn't drawing is left out.
 */
export function diagramNode(svg: string): SVGSVGElement {
  const parsed = new DOMParser().parseFromString(withoutInlineStyles(svg), "image/svg+xml");
  const root = parsed.documentElement;
  if (root.nodeName !== "svg" || parsed.querySelector("parsererror"))
    throw new Error("the drawing wasn't valid SVG");
  const styles: [string, string][][] = [];
  const clean = (element: Element) => {
    for (const child of [...element.children]) {
      if (DRAWING.has(child.localName)) clean(child);
      else child.remove();
    }
    // Attributes are live: read each name and value before changing any.
    for (const { name, value } of [...element.attributes].map(({ name, value }) => ({ name, value }))) {
      if (name === STYLE) {
        element.setAttribute(STYLE, String(styles.length));
        styles.push(styleDeclarations(value));
      } else if (name === "style" || !keepAttribute(name, value)) {
        element.removeAttribute(name);
      }
    }
  };
  clean(root);
  const node = document.importNode(root, true) as unknown as SVGSVGElement;
  const styled = [node, ...node.querySelectorAll(`[${STYLE}]`)].filter((element) =>
    element.hasAttribute(STYLE),
  ) as SVGElement[];
  for (const element of styled) {
    for (const [name, value] of styles[Number(element.getAttribute(STYLE))] ?? [])
      if (refersWithin(value)) element.style.setProperty(name, value);
    element.removeAttribute(STYLE);
  }
  return node;
}

/** What a Pele error says, in the shape a renderer reports it. */
export interface DiagramFailure {
  code?: string;
  mermaidType?: string | null;
  line?: number;
  snippet?: string;
  message?: string;
}

/** Why a diagram isn't drawn, as the note under its source says it. */
export function notDrawnReason(error: unknown, source: string): string {
  if (error instanceof DiagramRefused) return error.message;
  const failure = (typeof error === "object" && error !== null ? error : {}) as DiagramFailure;
  if (failure.code === "limit" && failure.message?.startsWith("Edge limit exceeded"))
    return `more than ${LIMITS.edges} edges`;
  switch (failure.code) {
    case "unsupported-diagram":
      if (failure.mermaidType) return `${failure.mermaidType} diagrams aren’t drawn yet`;
      return source.trim() ? "the first line names no diagram type" : "the diagram is empty";
    case "syntax": {
      const line = failure.line ? `line ${failure.line}` : "a line";
      const snippet = failure.snippet
        ?.split("\n")
        .map((value) => value.trim())
        .find((value) => value && !/^-*\^$/.test(value));
      return snippet ? `${line} doesn’t parse: ${snippet}` : `${line} doesn’t parse`;
    }
    case "semantic":
    case "limit":
      return sentence(failure.message) ?? "it couldn’t be drawn";
    default:
      return "it couldn’t be drawn";
  }
}

/** A message as one clause: its first line, sentences joined, without a capital or full stop. */
function sentence(message: string | undefined): string | undefined {
  const first = message
    ?.split("\n")[0]
    ?.trim()
    .replace(/\.$/, "")
    .replace(/\.\s+(\p{Lu})/gu, (_, letter: string) => `; ${letter.toLowerCase()}`);
  if (!first) return undefined;
  return first[0]?.toLowerCase() + first.slice(1);
}
