// Markdown parsing for transcript text. The tree is rendered element by
// element; no HTML from a session ever reaches the DOM as markup.
import type { Blockquote, Code, Paragraph, PhrasingContent, Root, RootContent } from "mdast";
import { fromMarkdown } from "mdast-util-from-markdown";
import { gfmFromMarkdown } from "mdast-util-gfm";
import { gfm } from "micromark-extension-gfm";

export function parseMarkdown(text: string): Root {
  return fromMarkdown(text, { extensions: [gfm()], mdastExtensions: [gfmFromMarkdown()] });
}

/** A link target the client opens: web and mail addresses only. */
export function safeHref(url: string): string | undefined {
  try {
    const parsed = new URL(url);
    return ["http:", "https:", "mailto:"].includes(parsed.protocol) ? parsed.href : undefined;
  } catch {
    return undefined;
  }
}

/** A GitHub alert's kind, as `[!KIND]` names it. */
export type CalloutKind = "note" | "tip" | "important" | "warning" | "caution";

/** A GitHub alert (`> [!WARNING] title`): its kind, title, and the blocks under it. */
export interface Callout {
  kind: CalloutKind;
  title: string;
  blocks: RootContent[];
}

const CALLOUT_KINDS = new Set<string>(["note", "tip", "important", "warning", "caution"]);

/** The kind and title a quote's first line marks, as `[!KIND] title`. */
export function calloutMarker(line: string): { kind: CalloutKind; title: string } | undefined {
  const text = line.trim();
  if (!text.startsWith("[!")) return undefined;
  const end = text.indexOf("]");
  if (end < 0) return undefined;
  const kind = text.slice(2, end).toLowerCase();
  if (!CALLOUT_KINDS.has(kind)) return undefined;
  const rest = text.slice(end + 1);
  if (rest !== "" && rest[0] !== " " && rest[0] !== "\t") return undefined;
  return { kind: kind as CalloutKind, title: rest.trim() };
}

/**
 * The callout a quote is, read as Kit's other clients read it: a quote whose
 * first line is `[!KIND]`, optionally followed by a title. The marker line
 * leaves the first paragraph; the rest of it and the quote's other blocks are
 * the body. Anything else stays a quote.
 */
export function calloutOf(quote: Blockquote, source: string): Callout | undefined {
  const first = quote.children[0];
  if (first?.type !== "paragraph") return undefined;
  let line = "";
  const rest: PhrasingContent[] = [];
  let ended = false;
  for (const node of first.children) {
    if (ended) {
      rest.push(node);
    } else if (node.type === "break") {
      ended = true;
    } else if (node.type === "text") {
      const newline = node.value.indexOf("\n");
      if (newline < 0) {
        line += node.value;
      } else {
        line += node.value.slice(0, newline);
        ended = true;
        const after = node.value.slice(newline + 1);
        if (after) rest.push({ type: "text", value: after });
      }
    } else {
      line += sourceOf(node, source);
    }
  }
  const marker = calloutMarker(line);
  if (!marker) return undefined;
  const blocks: RootContent[] = [];
  if (rest.length > 0) blocks.push({ type: "paragraph", children: rest } satisfies Paragraph);
  blocks.push(...quote.children.slice(1));
  return { ...marker, blocks };
}

function sourceOf(
  node: { position?: { start: { offset?: number }; end: { offset?: number } } },
  source: string,
) {
  const start = node.position?.start.offset;
  const end = node.position?.end.offset;
  return start === undefined || end === undefined ? "" : source.slice(start, end);
}

/**
 * Whether a fenced block's closing fence has arrived. A block still streaming
 * runs to the end of the text without one; Kit draws a figure or diagram only
 * once its fence closes, so nothing rescales mid-stream.
 */
export function fenceClosed(node: Code, source: string): boolean {
  const text = sourceOf(node, source);
  const lines = text.split("\n");
  const opening = /^ {0,3}(`{3,}|~{3,})/.exec(lines[0] ?? "");
  if (!opening?.[1] || lines.length < 2) return false;
  const fence = opening[1];
  const last = (lines[lines.length - 1] ?? "").trim();
  return last.length >= fence.length && last === (fence[0] ?? "").repeat(last.length);
}

/** A fenced block's whole info string: its language and the rest of the line. */
export function infoString(node: Code): string {
  return [node.lang, node.meta].filter(Boolean).join(" ");
}
