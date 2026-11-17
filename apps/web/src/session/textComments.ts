// Comments on selected text (ADR 0070): the blocks a client appends to a
// message for each comment a person made on words they selected, read back
// for the transcript. Held, like the Apple and terminal clients, to the
// fixtures in testdata/text-annotations.

export const OPEN = '<text-annotation n="';
export const CLOSE = "</text-annotation>";
const FENCE = "UNTRUSTED_SELECTED_TEXT";
/** The most of a quote a block holds, in UTF-8 bytes. */
export const MAX_QUOTE_BYTES = 4096;

/** A sent comment on selected text, as a transcript shows it. */
export interface SentTextComment {
  number: number;
  comment: string;
  /** Where the words came from: "Assistant reply", "Tool output · …". */
  source: string;
  quote: string;
  /** Whether the quote was cut to fit its block. */
  cut: boolean;
}

/**
 * A message's own text and the comment blocks after it: blocks numbered
 * from 1 that end the message, after its text and a blank line or alone. A
 * message without them comes back whole, as does one whose blocks are
 * malformed or followed by more text.
 */
export function splitTextComments(message: string): { text: string; comments: SentTextComment[] } {
  const first = `${OPEN}1">\n`;
  let from = 0;
  for (;;) {
    const found = message.indexOf(first, from);
    if (found < 0) return { text: message, comments: [] };
    const before = message.slice(0, found);
    if (before === "" || before.endsWith("\n\n")) {
      const comments = parseBlocks(message.slice(found));
      if (comments) return { text: before.trim(), comments };
    }
    from = found + first.length;
  }
}

function parseBlocks(text: string): SentTextComment[] | undefined {
  const comments: SentTextComment[] = [];
  let rest = text;
  for (;;) {
    const parsed = parse(rest, comments.length + 1);
    if (!parsed) return undefined;
    comments.push(parsed.comment);
    rest = rest.slice(parsed.end);
    if (rest.trim() === "") return comments;
    if (!rest.startsWith("\n\n")) return undefined;
    rest = rest.slice(2);
  }
}

/** The block numbered number at text's start and where it ends: only the fence with its own nonce closes it. */
function parse(text: string, number: number): { comment: SentTextComment; end: number } | undefined {
  const head = `${OPEN}${number}">\nComment: `;
  if (!text.startsWith(head)) return undefined;
  let rest = text.slice(head.length);
  let offset = head.length;
  const fenceStart = `\n[${FENCE} nonce=`;
  const fence = rest.indexOf(fenceStart);
  if (fence < 0) return undefined;
  const comment = rest.slice(0, fence).replaceAll("\n  ", "\n");
  rest = rest.slice(fence + fenceStart.length);
  offset += fence + fenceStart.length;
  const nonceEnd = rest.indexOf("]\n");
  if (nonceEnd < 0) return undefined;
  const nonce = rest.slice(0, nonceEnd);
  if (nonce === "" || nonce.includes("\n")) return undefined;
  const body = rest.slice(nonceEnd + 2);
  offset += nonceEnd + 2;
  const closing = `\n[/${FENCE} nonce=${nonce}]\n${CLOSE}`;
  const end = body.indexOf(closing);
  if (end < 0) return undefined;
  const inside = body.slice(0, end);
  const sourceEnd = inside.indexOf("\n");
  if (!inside.startsWith("From: ") || sourceEnd < 0) return undefined;
  const source = inside.slice("From: ".length, sourceEnd);
  const afterSource = inside.slice(sourceEnd + 1);
  const cutHead = `Text, cut to ${MAX_QUOTE_BYTES / 1024} KiB:\n`;
  let cut: boolean;
  let quote: string;
  if (afterSource.startsWith("Text:\n")) {
    cut = false;
    quote = afterSource.slice("Text:\n".length);
  } else if (afterSource.startsWith(cutHead)) {
    cut = true;
    quote = afterSource.slice(cutHead.length);
  } else return undefined;
  return { comment: { number, comment, source, quote, cut }, end: offset + end + closing.length };
}
