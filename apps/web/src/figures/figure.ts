// A parsed figure (ADR 0054): what every client draws, recorded in the
// shared fixtures. Values are kept as written; the numbers each kind derives
// from them, such as a bar's length, are computed here as the terminal
// client's reference computes them. Ported from the macOS client's
// `KitFigures`, kind for kind, so the reasons a figure isn't drawn match.
import * as G from "./grammar";
import { attribute, fail, type Info, type Item as Line, NotDrawn, ok, type Result } from "./grammar";

export { NotDrawn };

export const KINDS = [
  "stat",
  "bars",
  "meter",
  "timeline",
  "steps",
  "checklist",
  "decision",
  "delta",
  "spec",
  "tree",
  "compare",
  "score",
  "stack",
] as const;

export type Kind = (typeof KINDS)[number];

/**
 * Where a timeline item or step stands against the bold one: before it, at
 * it, after it, or none when nothing is bold. A checklist task is done, now
 * when bold, or open.
 */
export type State = "" | "done" | "now" | "next" | "open";

/**
 * Whether a delta row's change is for the better or the worse, or none when
 * the figure doesn't say which way is better or nothing changed.
 */
export type Trend = "" | "better" | "worse";

/** One row, column, or point of a figure. */
export interface FigureItem {
  label: string;
  /** The value as written. */
  value: string;
  note: string;
  /** The item to read, which takes the accent. */
  accent: boolean;
  /** An item that recedes. */
  muted: boolean;
  /** A bar's length, in thousandths of the track. */
  scale: number;
  /** A bar's share, such as `33%`, or empty. */
  share: string;
  /** A stat's sparkline, each point in thousandths of the line's range. */
  spark: number[];
  /** A stat value too long to draw large, drawn at the label's size instead. */
  small: boolean;
  /** A meter row's target as written, or empty. */
  target: string;
  /** A meter past its target or past 100%, which turns warning. */
  over: boolean;
  state: State;
  /** A timeline item's, step's, or task's indented body, its lines joined. */
  body: string;
  /** A delta row's value before, as written, or empty. */
  from: string;
  /** A delta row's sign as shown, `+` or `−`, or empty. */
  sign: string;
  /** A delta row's computed change, or empty. */
  change: string;
  trend: Trend;
  /** A tree node's depth, from 1 at the top level, or 0. */
  level: number;
  /** A compare row's cells as written, one per option. */
  cells: string[];
  /** A score's filled half dots, and its dots, or 0. */
  filled: number;
  slots: number;
  /** A stack row's segments, each in thousandths of the row's total. */
  parts: number[];
}

export interface Figure {
  kind: Kind;
  title: string;
  items: FigureItem[];
  /** A decision's status, or empty. */
  status: string;
  /** A decision's date, or empty. */
  date: string;
  /** A decision's closing paragraph, or empty. */
  note: string;
  /** Column headings: a delta's from and to, a compare's options, or a stack's segment names. */
  headings: string[];
  /** The compare option that is the pick, or -1. */
  pick: number;
}

function item(fields: Partial<FigureItem> & { label: string; value: string }): FigureItem {
  return {
    note: "",
    accent: false,
    muted: false,
    scale: 0,
    share: "",
    spark: [],
    small: false,
    target: "",
    over: false,
    state: "",
    body: "",
    from: "",
    sign: "",
    change: "",
    trend: "",
    level: 0,
    cells: [],
    filled: 0,
    slots: 0,
    parts: [],
    ...fields,
  };
}

function figure(kind: Kind, info: Info, items: FigureItem[], extra: Partial<Figure> = {}): Figure {
  return {
    kind,
    title: attribute(info, "title"),
    items,
    status: "",
    date: "",
    note: "",
    headings: [],
    pick: -1,
    ...extra,
  };
}

/** The widest label, cell, or heading drawn, in characters. */
export const MAX_LABEL = 32;

/** The fenced source of a figure, as an author would paste it back. */
export function fenced(info: string, body: string): string {
  const longest = Math.max(0, ...(body.match(/`+/g) ?? []).map((run) => run.length));
  const fence = "`".repeat(Math.max(3, longest + 1));
  return `${fence}${info}\n${body}\n${fence}`;
}

const segmenter = typeof Intl.Segmenter === "function" ? new Intl.Segmenter() : undefined;

/** text cut to limit characters, ending in `…` when it is longer. */
export function cut(text: string, limit = MAX_LABEL): string {
  const characters = segmenter ? [...segmenter.segment(text)].map((each) => each.segment) : [...text];
  return characters.length > limit ? `${characters.slice(0, limit - 1).join("")}…` : text;
}

/** Whether a fenced code block's info string names a figure. */
export function isFigure(info: string): boolean {
  return (KINDS as readonly string[]).includes(G.name(info));
}

/** The figure a fence describes, or why it can't be drawn. */
export function parseFigure(info: string, body: string): Result<Figure> {
  const parsed = G.parseInfo(info);
  if (!parsed.ok) return parsed;
  const read = PARSERS[parsed.value.name as Kind];
  return read ? read(parsed.value, body) : fail(`\`${parsed.value.name}\` figures aren't drawn yet`);
}

/** The figure as fixture text, one line per field and item, as the reference records it. */
export function record(figure: Figure): string {
  const quoted = (text: string) => `"${text.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
  const lines = [`figure ${figure.kind}`, `title ${quoted(figure.title)}`];
  if (figure.status) lines.push(`status ${figure.status}`);
  if (figure.date) lines.push(`date ${quoted(figure.date)}`);
  if (figure.note) lines.push(`note ${quoted(figure.note)}`);
  if (figure.headings.length) lines.push(`headings ${figure.headings.map(quoted).join(",")}`);
  if (figure.pick >= 0) lines.push(`pick ${figure.pick}`);
  for (const each of figure.items) {
    let extra = "";
    if (each.small) extra += " small=true";
    if (each.target) extra += ` target=${quoted(each.target)}`;
    if (each.over) extra += " over=true";
    if (each.state) extra += ` state=${each.state}`;
    if (each.body) extra += ` body=${quoted(each.body)}`;
    if (each.from) extra += ` from=${quoted(each.from)}`;
    if (each.sign) extra += ` sign=${quoted(each.sign)}`;
    if (each.change) extra += ` change=${quoted(each.change)}`;
    if (each.trend) extra += ` trend=${each.trend}`;
    if (each.level > 0) extra += ` level=${each.level}`;
    if (each.cells.length) extra += ` cells=${each.cells.map(quoted).join(",")}`;
    if (each.slots > 0) extra += ` filled=${each.filled} slots=${each.slots}`;
    if (each.parts.length) extra += ` parts=${each.parts.join(",")}`;
    lines.push(
      `item label=${quoted(each.label)} value=${quoted(each.value)} note=${quoted(each.note)} ` +
        `accent=${each.accent} muted=${each.muted} scale=${each.scale} share=${quoted(each.share)} ` +
        `spark=${each.spark.join(",")}${extra}`,
    );
  }
  return `${lines.join("\n")}\n`;
}

/** A failure carried on as another result's type. */
const passOn = <T>(error: NotDrawn): Result<T> => ({ ok: false, error });

// bars: one labelled bar per row, scaled to the largest value or `max`, with
// each row's share of the first row or of the total when `percent` asks.

const BARS_MAX_ROWS = 20;
/** The largest share shown; a larger one shows as `>9999%`. */
const MAX_SHARE = 9999;

function share(value: number, whole: number): string {
  const percent = (value / whole) * 100;
  if (percent === 0) return "0%";
  if (percent < 1) return "<1%";
  if (percent >= MAX_SHARE + 0.5) return `>${MAX_SHARE}%`;
  return `${G.rounded(percent)}%`;
}

function bars(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title", "max", "percent"]);
  if (unknown) return passOn(unknown);
  const listed = G.items(body);
  if (!listed.ok) return listed;
  if (listed.value.filter((each) => each.bold).length > 1) return fail("only one item can be bold");
  const rows: { item: FigureItem; number: number }[] = [];
  for (const each of listed.value) {
    const pair = G.labelValue(each.text);
    if (!pair) return fail(`a bars row is \`label: value\`, got \`${G.clip(each.text)}\``);
    const [label, value] = pair;
    const parsed = G.number(value);
    if (!parsed.ok) return parsed;
    if (each.details.length) return fail("a bars row takes no indented lines");
    if (parsed.value < 0) return fail(`bars can't be negative, got \`${G.clip(value)}\``);
    rows.push({
      item: item({ label, value, note: each.note, accent: each.bold, muted: each.italic }),
      number: parsed.value,
    });
  }
  let most: number;
  const max = attribute(info, "max");
  if (max === "") {
    most = rows.reduce((largest, row) => Math.max(largest, row.number), 0);
  } else {
    const parsed = G.number(max);
    if (!parsed.ok) return parsed;
    if (!(parsed.value > 0)) return fail("`max` must be above zero");
    most = parsed.value;
  }
  const percent = attribute(info, "percent");
  const total = rows.reduce((sum, row) => sum + row.number, 0);
  const first = rows[0]?.number ?? 0;
  let whole = 0;
  if (percent === "first") {
    if (!(first > 0)) return fail('`percent="first"` needs a first row above zero');
    whole = first;
  } else if (percent === "total") {
    if (!(total > 0)) return fail('`percent="total"` needs a total above zero');
    whole = total;
  } else if (percent !== "") {
    return fail("`percent` is `first` or `total`");
  }
  const made = rows.map((row, index) => ({
    ...row.item,
    scale: G.permille(row.number, most),
    share: whole > 0 && !(percent === "first" && index === 0) ? share(row.number, whole) : "",
  }));
  if (rows.length === 0) return fail("the figure has no rows");
  if (rows.length > BARS_MAX_ROWS) return fail(`a bars figure has at most ${BARS_MAX_ROWS} rows`);
  return ok(figure("bars", info, made));
}

// stat: two to four large numbers side by side, each a value then a label,
// with an optional note and a sparkline from a line of numbers under it.

const STAT_MAX_ITEMS = 4;
// The longest value drawn large, in characters. A longer value is drawn at
// the label's size, unless it is a before and after.
const STAT_MAX_VALUE = 16;
const STAT_MAX_POINTS = 60;

function spark(details: string[]): Result<number[]> {
  if (details.length === 0) return ok([]);
  if (details.length > 1) return fail("a stat item takes one line of numbers under it");
  const values = G.numbers(details[0] ?? "", STAT_MAX_POINTS);
  if (!values.ok) return values;
  if (values.value.length < 2) return fail("a sparkline needs at least 2 values");
  const low = Math.min(...values.value);
  const high = Math.max(...values.value);
  return ok(values.value.map((value) => (high > low ? G.permille(value - low, high - low) : 500)));
}

function statItem(each: Line): Result<FigureItem> {
  const text = each.text;
  const afterOpening = text.slice(2);
  const close = G.index(afterOpening, "**");
  const leading = !each.bold && text.startsWith("**") && (close ?? 0) > 0;
  const first = G.fields(text)[0] ?? "";
  let value: string;
  let label: string;
  if (leading && close !== undefined) {
    value = afterOpening.slice(0, close);
    label = G.trimSpace(afterOpening.slice(close + 2));
  } else {
    value = first;
    label = G.trimSpace(text.slice(first.length));
  }
  const points = spark(each.details);
  if (!points.ok) return points;
  if (value === "" || label === "")
    return fail(`a stat item is a value then a label, got \`${G.clip(text)}\``);
  const small = [...value].length > STAT_MAX_VALUE;
  if (small && beforeAndAfter(value))
    return fail(
      `a stat value with an arrow is at most ${STAT_MAX_VALUE} characters, got \`${G.clip(value)}\`; ` +
        "a before and after belongs in a delta figure",
    );
  return ok(
    item({
      label,
      value,
      note: each.note,
      accent: each.bold || leading,
      muted: each.italic,
      spark: points.value,
      small,
    }),
  );
}

/** Whether a value reads as a before and after, such as `8s → 2s`. */
function beforeAndAfter(value: string): boolean {
  return value.includes("→") || value.includes("->");
}

function stat(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  const listed = G.items(body);
  if (!listed.ok) return listed;
  const made: FigureItem[] = [];
  for (const each of listed.value) {
    const parsed = statItem(each);
    if (!parsed.ok) return parsed;
    made.push(parsed.value);
  }
  if (listed.value.length === 0) return fail("the figure has no items");
  if (listed.value.length > STAT_MAX_ITEMS) return fail(`a stat figure has at most ${STAT_MAX_ITEMS} items`);
  if (made.filter((each) => each.accent).length > 1) return fail("only one item can be bold");
  return ok(figure("stat", info, made));
}

// meter: one share of a whole, or rows each measured against its own
// target, with the targets lined up in one column.

const METER_MAX_ROWS = 12;

/** A row's value and target, split at ` / `. */
function fraction(text: string): [string, string] | undefined {
  const at = G.index(text, " / ");
  if (at === undefined || at === 0) return undefined;
  return [G.trimSpace(text.slice(0, at)), G.trimSpace(text.slice(at + 3))];
}

/** A value and the target it is measured against. */
function against(value: string, target: string): Result<[number, number]> {
  const amount = G.number(value);
  if (!amount.ok) return amount;
  const whole = G.number(target);
  if (!whole.ok) return whole;
  if (amount.value < 0) return fail(`a meter can't be negative, got \`${G.clip(value)}\``);
  if (whole.value <= 0) return fail(`a meter's target must be above zero, got \`${G.clip(target)}\``);
  return ok([amount.value, whole.value]);
}

/** A single meter's share of its whole: `N%`, a fraction from 0 to 1, or `a / b`. */
function ratio(text: string): Result<number> {
  const pair = fraction(text);
  if (pair) {
    const measured = against(pair[0], pair[1]);
    return measured.ok ? ok(measured.value[0] / measured.value[1]) : measured;
  }
  const value = G.number(text);
  if (!value.ok) return value;
  if (value.value < 0) return fail(`a meter can't be negative, got \`${G.clip(text)}\``);
  if (text.endsWith("%")) return ok(value.value / 100);
  if (value.value > 1)
    return fail(`a meter is \`N%\`, a fraction from 0 to 1, or \`a / b\`, got \`${G.clip(text)}\``);
  return ok(value.value);
}

function meter(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  const lines = body
    .split("\n")
    .map(G.trimSpace)
    .filter((line) => line !== "");
  const first = lines[0] ?? "";
  if (G.byteLength(body) > G.MAX_BODY) return fail(`the figure is larger than ${G.MAX_BODY / 1024} KiB`);
  if (lines.length === 0) return fail("the figure has no rows");
  if (first.startsWith("- ") || G.isOrdered(first)) return meterRows(info, body);
  if (lines.length > 1) return fail("a single meter is one line, as in `67% — note`; rows are a list");
  const each = G.line(first);
  const share = ratio(each.text);
  if (!share.ok) return share;
  const made = item({
    label: "",
    value: each.text,
    note: each.note,
    accent: true,
    scale: G.permille(share.value, 1),
    over: share.value > 1,
  });
  return ok(figure("meter", info, [made]));
}

function meterRows(info: Info, body: string): Result<Figure> {
  const listed = G.items(body);
  if (!listed.ok) return listed;
  if (listed.value.filter((each) => each.bold).length > 1) return fail("only one item can be bold");
  const made: FigureItem[] = [];
  for (const each of listed.value) {
    const pair = G.labelValue(each.text);
    if (!pair) return fail(`a meter row is \`label: value / target\`, got \`${G.clip(each.text)}\``);
    const [label, written] = pair;
    const parts = fraction(written);
    if (!parts)
      return fail(`a meter row needs a target, as in \`label: 9 / 12\`, got \`${G.clip(each.text)}\``);
    const measured = against(parts[0], parts[1]);
    if (!measured.ok) return measured;
    if (each.details.length) return fail("a meter row takes no indented lines");
    const [amount, whole] = measured.value;
    made.push(
      item({
        label,
        value: parts[0],
        note: each.note,
        accent: each.bold,
        muted: each.italic,
        target: parts[1],
        scale: G.permille(amount, whole * 1.25),
        over: amount > whole,
      }),
    );
  }
  if (made.length === 0) return fail("the figure has no rows");
  if (made.length > METER_MAX_ROWS) return fail(`a meter figure has at most ${METER_MAX_ROWS} rows`);
  return ok(figure("meter", info, made));
}

/** Where each item stands against the first bold one. */
function states(items: Line[]): State[] {
  const found = items.findIndex((each) => each.bold);
  if (found < 0) return items.map(() => "");
  return items.map((_, index) => (index < found ? "done" : index === found ? "now" : "next"));
}

// timeline: dated events in order, each `when: event` with an optional
// indented body. The bold item is now; items before it are done.

const TIMELINE_MAX_ITEMS = 20;

function timeline(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  const listed = G.items(body);
  if (!listed.ok) return listed;
  if (listed.value.filter((each) => each.bold).length > 1) return fail("only one item can be bold");
  const standing = states(listed.value);
  const made: FigureItem[] = [];
  for (const [index, each] of listed.value.entries()) {
    const pair = G.labelValue(each.text);
    if (!pair) return fail(`a timeline item is \`when: event\`, got \`${G.clip(each.text)}\``);
    made.push(
      item({
        label: pair[0],
        value: pair[1],
        note: each.note,
        accent: each.bold,
        muted: each.italic,
        body: each.details.join(" "),
        state: standing[index] ?? "",
      }),
    );
  }
  if (made.length === 0) return fail("the figure has no items");
  if (made.length > TIMELINE_MAX_ITEMS) return fail(`a timeline has at most ${TIMELINE_MAX_ITEMS} items`);
  return ok(figure("timeline", info, made));
}

// steps: an ordered list of steps, each a title with an optional indented
// body. The bold step is current; the steps before it are done.

const STEPS_MAX_ITEMS = 20;

function steps(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  const listed = G.items(body);
  if (!listed.ok) return listed;
  const all = listed.value;
  if (all.filter((each) => each.bold).length > 1) return fail("only one item can be bold");
  const standing = states(all);
  const first = all[0]?.ordinal ?? 1;
  const made = all.map((each, index) =>
    item({
      label: String(first + index),
      value: each.text,
      note: each.note,
      accent: each.bold,
      muted: each.italic,
      body: each.details.join(" "),
      state: standing[index] ?? "",
    }),
  );
  if (all.some((each) => task(each.text)))
    return fail("steps take no `[ ]` or `[x]` boxes; a list of tasks is a `checklist` figure");
  const unordered = all.find((each) => each.ordinal < 0);
  if (unordered) return fail(`steps are an ordered list, as in \`1. ${G.clip(unordered.text)}\``);
  if (made.length === 0) return fail("the figure has no steps");
  if (made.length > STEPS_MAX_ITEMS) return fail(`a steps figure has at most ${STEPS_MAX_ITEMS} steps`);
  return ok(figure("steps", info, made));
}

// checklist: tasks done in any order, each `[ ]` or `[x]` with a title and
// an optional indented body, and one level of subtasks indented under a
// task. The bold task is the one in hand, which is never checked.

const CHECKLIST_MAX_ITEMS = 20;

/** text's leading `[ ]`, `[x]`, or `[X]` box and the title after it, or undefined. */
function task(text: string): { done: boolean; title: string } | undefined {
  const box = text.slice(0, 3);
  const after = text.slice(3);
  const done = box === "[x]" || box === "[X]";
  if (!(box === "[ ]" || done) || !(after === "" || after.startsWith(" "))) return undefined;
  return { done, title: G.trimSpace(after) };
}

/**
 * each's indented lines split into its body and its subtasks, which all
 * share one indent. A box line deeper than that is a second level, and prose
 * after a subtask is a subtask's body; neither is drawn.
 */
function under(each: Line): Result<{ body: string[]; subtasks: Line[] }> {
  const body: string[] = [];
  const subtasks: Line[] = [];
  // The indent the task's subtasks share, once one is read.
  let depth = -1;
  for (const [index, line] of each.details.entries()) {
    const at = each.depths[index] ?? 0;
    const subtask = G.listItem(line);
    if (subtask) {
      if (depth < 0) depth = at;
      if (at > depth) return fail(`a checklist nests one level, got \`${G.clip(line)}\``);
      if (at < depth) return fail(`a task's subtasks are indented alike, got \`${G.clip(line)}\``);
      subtasks.push(subtask);
    } else {
      if (depth >= 0)
        return fail(
          at > depth
            ? `a subtask takes no indented body, got \`${G.clip(line)}\``
            : `a task's body comes before its subtasks, got \`${G.clip(line)}\``,
        );
      body.push(line);
    }
  }
  return ok({ body, subtasks });
}

/**
 * each as a task at level, 1 or 2 for a subtask, with body under it: its
 * box split off, then the emphasis around the title after it, as in
 * `- [ ] **title**`.
 */
function entry(each: Line, level: number, body: string[]): Result<FigureItem> {
  const boxed = task(each.text);
  if (!boxed) return fail(`a checklist item starts with \`[ ]\` or \`[x]\`, got \`${G.clip(each.text)}\``);
  // Emphasis around the box, as in `- **[ ] title**`, was already read.
  const marked = each.bold || each.italic;
  const inner = G.emphasis(boxed.title);
  const title = marked ? boxed.title : inner.text;
  const accent = each.bold || (!marked && inner.bold);
  const made = item({
    label: "",
    value: title,
    note: each.note,
    accent,
    muted: each.italic || (!marked && inner.italic),
    body: body.join(" "),
    level,
    state: boxed.done ? "done" : accent ? "now" : "open",
  });
  if (title === "") return fail("a checklist task needs a title, as in `- [ ] Ship`");
  if (boxed.done && accent)
    return fail(`the bold task is the one in hand, so it can't be checked: \`${G.clip(title)}\``);
  return ok(made);
}

function checklist(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  const listed = G.items(body);
  if (!listed.ok) return listed;
  const made: FigureItem[] = [];
  for (const each of listed.value) {
    const split = under(each);
    if (!split.ok) return split;
    const tasks: [Line, number, string[]][] = [
      [each, 1, split.value.body],
      ...split.value.subtasks.map((subtask): [Line, number, string[]] => [subtask, 2, []]),
    ];
    for (const [line, level, lines] of tasks) {
      const parsed = entry(line, level, lines);
      if (!parsed.ok) return parsed;
      made.push(parsed.value);
    }
  }
  if (made.filter((each) => each.accent).length > 1) return fail("only one item can be bold");
  if (made.length === 0) return fail("the figure has no tasks");
  if (made.length > CHECKLIST_MAX_ITEMS) return fail(`a checklist has at most ${CHECKLIST_MAX_ITEMS} tasks`);
  return ok(figure("checklist", info, made));
}

// decision: the options weighed, the chosen one bold and those ruled out in
// italics, each with its reason, under the decision's status and date and
// over a closing note.

const DECISION_MIN = 2;
const DECISION_MAX = 8;
const STATUSES = ["proposed", "accepted", "rejected", "superseded"];

function decision(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title", "status", "date"]);
  if (unknown) return passOn(unknown);
  const status = attribute(info, "status");
  if (status !== "" && !STATUSES.includes(status))
    return fail("`status` is `proposed`, `accepted`, `rejected` or `superseded`");
  const listed = G.itemsAndNote(body);
  if (!listed.ok) return listed;
  const options = listed.value.items;
  const bold = G.checkOneBold(options);
  if (bold) return passOn(bold);
  if (options.some((each) => each.details.length)) return fail("a decision's options take no indented lines");
  if (options.length < DECISION_MIN || options.length > DECISION_MAX)
    return fail(`a decision has ${DECISION_MIN} to ${DECISION_MAX} options, got ${options.length}`);
  const made = options.map((each) =>
    item({ label: each.text, value: "", note: each.note, accent: each.bold, muted: each.italic }),
  );
  return ok(
    figure("decision", info, made, { status, date: attribute(info, "date"), note: listed.value.note }),
  );
}

// spec: labelled values in one aligned column, the values wrapping as prose,
// each with an optional indented note under it.

const SPEC_MAX_ROWS = 30;

function spec(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  const listed = G.items(body);
  if (!listed.ok) return listed;
  const bold = G.checkOneBold(listed.value);
  if (bold) return passOn(bold);
  const made: FigureItem[] = [];
  for (const each of listed.value) {
    const pair = G.labelValue(each.text);
    if (!pair) return fail(`a spec row is \`label: value\`, got \`${G.clip(each.text)}\``);
    made.push(
      item({
        label: pair[0],
        value: pair[1],
        note: each.note,
        accent: each.bold,
        muted: each.italic,
        body: each.details.join(" "),
      }),
    );
  }
  if (made.length === 0) return fail("the figure has no rows");
  if (made.length > SPEC_MAX_ROWS) return fail(`a spec has at most ${SPEC_MAX_ROWS} rows`);
  return ok(figure("spec", info, made));
}

// delta: what changed, either signed values with an optional total or values
// before and after, with an optional computed change.

const DELTA_MAX_ROWS = 20;
/** The largest percent change shown; a larger one shows as `>9999%`. */
const MAX_PERCENT = 9999;

/** A row's value before and after, split at ` → ` or ` -> `. */
function change(text: string): [string, string] | undefined {
  const arrow = G.index(text, " → ");
  const [at, width] = arrow !== undefined ? [arrow, 3] : [G.index(text, " -> "), 4];
  if (at === undefined || at === 0) return undefined;
  return [G.trimSpace(text.slice(0, at)), G.trimSpace(text.slice(at + width))];
}

function trend(difference: number, better: string): Trend {
  if (better === "" || difference === 0) return "";
  return difference > 0 === (better === "up") ? "better" : "worse";
}

/** The change from before to after as a whole percent of before, `–` when before is zero. */
function percentChange(before: number, after: number): string {
  if (before === 0) return "–";
  const share = ((after - before) / Math.abs(before)) * 100;
  const whole = G.rounded(share);
  const sign = whole > 0 ? "+" : whole < 0 ? "−" : "";
  if (Math.abs(share) >= MAX_PERCENT + 0.5) return `${sign}>${MAX_PERCENT}%`;
  return `${sign}${Math.abs(whole)}%`;
}

function choice(info: Info, key: string, allowed: string[]): NotDrawn | undefined {
  const value = attribute(info, key);
  if (value === "" || allowed.includes(value)) return undefined;
  return new NotDrawn(`\`${key}\` is \`${allowed.join("` or `")}\``);
}

function delta(info: Info, body: string): Result<Figure> {
  const unknown =
    G.checkAttributes(info, ["title", "from", "to", "change", "better"]) ??
    choice(info, "change", ["percent", "absolute"]) ??
    choice(info, "better", ["up", "down"]);
  if (unknown) return passOn(unknown);
  const better = attribute(info, "better");
  const computed = attribute(info, "change");
  const listed = G.items(body);
  if (!listed.ok) return listed;
  const made: FigureItem[] = [];
  let paired = 0;
  for (const each of listed.value) {
    const pair = G.labelValue(each.text);
    if (!pair)
      return fail(`a delta row is \`label: value\` or \`label: from → to\`, got \`${G.clip(each.text)}\``);
    const [label, writtenValue] = pair;
    const row = item({ label, value: writtenValue, note: each.note, accent: each.bold, muted: each.italic });
    if (each.details.length) return fail("a delta row takes no indented lines");
    const changed = change(writtenValue);
    if (changed) {
      const [before, after] = changed;
      const from = G.number(before);
      if (!from.ok) return from;
      const to = G.number(after);
      if (!to.ok) return to;
      if (!G.sameUnit(G.shape(before), G.shape(after)))
        return fail(
          `a delta row's values before and after are in the same unit, got \`${G.clip(writtenValue)}\``,
        );
      row.from = before;
      row.value = after;
      row.trend = trend(to.value - from.value, better);
      if (computed === "percent") row.change = percentChange(from.value, to.value);
      else if (computed === "absolute")
        row.change = G.written(to.value - from.value, G.common([G.shape(after), G.shape(before)]), true);
      paired += 1;
    } else {
      const value = G.number(writtenValue);
      if (!value.ok) return value;
      const signed = writtenValue.startsWith("+") || writtenValue.startsWith("-");
      if (signed) row.value = G.trimSpace(writtenValue.slice(1));
      // A signed zero changed nothing, so it has no sign or trend.
      if (signed && value.value !== 0) {
        const negative = writtenValue.startsWith("-");
        row.sign = value.value < 0 || negative ? "−" : "+";
        row.trend = trend(negative ? -1 : 1, better);
      }
    }
    made.push(row);
  }
  if (made.length === 0) return fail("the figure has no rows");
  if (made.length > DELTA_MAX_ROWS) return fail(`a delta figure has at most ${DELTA_MAX_ROWS} rows`);
  if (paired > 0 && paired < made.length)
    return fail("a delta's rows are all `label: value` or all `label: from → to`");
  if (made.slice(0, -1).some((each) => each.accent))
    return fail("only the last row can be bold, as the total");
  if (paired === 0) {
    const key = ["from", "to", "change"].find((each) => attribute(info, each) !== "");
    if (key) return fail(`\`${key}\` needs rows of \`label: from → to\``);
  }
  const from = attribute(info, "from");
  const to = attribute(info, "to");
  return ok(figure("delta", info, made, { headings: from || to ? [from, to] : [] }));
}

// score: rows rated out of the same whole, each drawn as dots with the value
// as written.

const SCORE_MAX_ROWS = 20;
const SCORE_MAX_SLOTS = 10;
const RATING = /^([0-9]{1,2})(\.[05])? ?\/ ?([0-9]{1,2})$/;

/** A score's halves and its whole, from `N/M`. */
function rating(text: string): Result<{ halves: number; slots: number }> {
  const match = RATING.exec(text);
  if (!match) return fail(`a score is \`N/M\`, got \`${G.clip(text)}\``);
  const slots = Number(match[3]);
  const halves = Number(match[1]) * 2 + (match[2] === ".5" ? 1 : 0);
  if (slots < 1 || slots > SCORE_MAX_SLOTS)
    return fail(`a score is out of 1 to ${SCORE_MAX_SLOTS}, got \`${G.clip(text)}\``);
  if (halves > slots * 2) return fail(`a score can't be more than its whole, got \`${G.clip(text)}\``);
  return ok({ halves, slots });
}

function score(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  const listed = G.items(body);
  if (!listed.ok) return listed;
  const bold = G.checkOneBold(listed.value);
  if (bold) return passOn(bold);
  const made: FigureItem[] = [];
  for (const each of listed.value) {
    const pair = G.labelValue(each.text);
    if (!pair) return fail(`a score row is \`label: N/M\`, got \`${G.clip(each.text)}\``);
    const rated = rating(pair[1]);
    if (!rated.ok) return rated;
    if (each.details.length) return fail("a score row takes no indented lines");
    made.push(
      item({
        label: pair[0],
        value: pair[1],
        note: each.note,
        accent: each.bold,
        muted: each.italic,
        filled: rated.value.halves,
        slots: rated.value.slots,
      }),
    );
  }
  if (made.length === 0) return fail("the figure has no rows");
  if (made.length > SCORE_MAX_ROWS) return fail(`a score figure has at most ${SCORE_MAX_ROWS} rows`);
  if (new Set(made.map((each) => each.slots)).size > 1) return fail("every score is out of the same whole");
  return ok(figure("score", info, made));
}

// stack: rows split into the same named segments, each row filling the track
// or, with `scale="shared"`, scaled to the largest total.

const STACK_MAX_ROWS = 12;
const STACK_MIN_SEGMENTS = 2;
const STACK_MAX_SEGMENTS = 4;

interface Segment {
  name: string;
  written: string;
  value: number;
}

function segment(text: string): Result<Segment> {
  const trimmed = G.trimSpace(text);
  const space = G.index(trimmed, " ");
  if (space === undefined || space === 0)
    return fail(`a stack segment is \`value name\`, got \`${G.clip(trimmed)}\``);
  const written = trimmed.slice(0, space);
  const name = G.trimSpace(trimmed.slice(space + 1));
  if (name === "") return fail(`a stack segment is \`value name\`, got \`${G.clip(trimmed)}\``);
  const value = G.number(written);
  if (!value.ok) return value;
  if (value.value < 0) return fail(`stack values can't be negative, got \`${G.clip(written)}\``);
  return ok({ name, written, value: value.value });
}

function stack(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title", "scale"]);
  if (unknown) return passOn(unknown);
  const scale = attribute(info, "scale");
  const listed = G.items(body);
  if (!listed.ok) return listed;
  const bold = G.checkOneBold(listed.value);
  if (bold) return passOn(bold);
  const rows: { item: FigureItem; names: string[]; total: number; shapes: G.Shape[] }[] = [];
  for (const each of listed.value) {
    const pair = G.labelValue(each.text);
    if (!pair) return fail(`a stack row is \`label: value name, …\`, got \`${G.clip(each.text)}\``);
    const segments: Segment[] = [];
    for (const text of pair[1].split(", ")) {
      const parsed = segment(text);
      if (!parsed.ok) return parsed;
      segments.push(parsed.value);
    }
    if (each.details.length) return fail("a stack row takes no indented lines");
    if (segments.length < STACK_MIN_SEGMENTS || segments.length > STACK_MAX_SEGMENTS)
      return fail(
        `a stack row has ${STACK_MIN_SEGMENTS} to ${STACK_MAX_SEGMENTS} segments, got ${segments.length}`,
      );
    const total = segments.reduce((sum, each) => sum + each.value, 0);
    const shapes = segments.map((each) => G.shape(each.written));
    rows.push({
      item: item({
        label: pair[0],
        value: G.written(total, G.common(shapes), false),
        note: each.note,
        accent: each.bold,
        muted: each.italic,
        parts: segments.map((each) => G.permille(each.value, total)),
      }),
      names: segments.map((each) => each.name),
      total,
      shapes,
    });
  }
  if (scale !== "" && scale !== "shared") return fail("`scale` is `shared`");
  if (rows.length === 0) return fail("the figure has no rows");
  if (rows.length > STACK_MAX_ROWS) return fail(`a stack figure has at most ${STACK_MAX_ROWS} rows`);
  const headings = rows[0]?.names ?? [];
  const different = rows.find((row) => row.names.join("\u0000") !== headings.join("\u0000"));
  if (different)
    return fail(
      `every stack row has the segments \`${G.clip(headings.join(", "))}\` in order, unlike \`${G.clip(different.item.label)}\``,
    );
  const unit = rows[0]?.shapes[0];
  const unlike = unit && rows.find((row) => !row.shapes.every((each) => G.sameUnit(each, unit)));
  if (unlike) return fail(`every stack value is in the same unit, unlike \`${G.clip(unlike.item.label)}\``);
  const largest = Math.max(0, ...rows.map((row) => row.total));
  const made = rows.map((row) => ({
    ...row.item,
    scale: scale === "shared" ? G.permille(row.total, largest) : row.total > 0 ? 1000 : 0,
  }));
  return ok(figure("stack", info, made, { headings }));
}

// tree: nested `- ` items, two spaces per level, drawn with guides from each
// node to its parent. Any number of nodes may be bold.

const TREE_MAX_NODES = 200;
const TREE_MAX_DEPTH = 8;
// `.` matches line separators such as U+2028, as it does in Go.
const NODE = /^([ \t]*)- (.*)$/s;

function tree(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  if (G.byteLength(body) > G.MAX_BODY) return fail(`the figure is larger than ${G.MAX_BODY / 1024} KiB`);
  const made: FigureItem[] = [];
  let previous = 0;
  for (const raw of G.lines(body)) {
    const line = raw.replace(/[ \t\r]+$/, "");
    const trimmed = G.trimSpace(line);
    if (trimmed === "") continue;
    const match = NODE.exec(line);
    if (!match) return fail(`expected a \`- \` tree node, got \`${G.clip(trimmed)}\``);
    const indent = match[1] ?? "";
    const level = Math.floor(indent.length / 2) + 1;
    if (indent.includes("\t") || indent.length % 2 === 1)
      return fail(`a tree is indented with two spaces per level, got \`${G.clip(trimmed)}\``);
    if (level > previous + 1)
      return fail(`a node is nested more than one level below its parent, got \`${G.clip(trimmed)}\``);
    if (level > TREE_MAX_DEPTH) return fail(`a tree is at most ${TREE_MAX_DEPTH} levels deep`);
    const each = G.line(match[2] ?? "");
    if (each.text === "") return fail(`a tree node needs a name, got \`${G.clip(trimmed)}\``);
    made.push(
      item({ label: each.text, value: "", note: each.note, accent: each.bold, muted: each.italic, level }),
    );
    previous = level;
  }
  if (made.length === 0) return fail("the figure has no nodes");
  if (made.length > TREE_MAX_NODES) return fail(`a tree has at most ${TREE_MAX_NODES} nodes`);
  return ok(figure("tree", info, made));
}

/**
 * One column of the guides before a tree node, for each level above it: a
 * line that runs on to a later sibling, none, or for the node's own level, a
 * line that goes on past it or ends at it.
 */
export type Guide = "none" | "through" | "branch" | "last";

/** Whether the node at index has a later sibling: a node at its level before any node above it. */
function continues(items: FigureItem[], index: number, level: number): boolean {
  for (const next of items.slice(index + 1)) {
    if (next.level < level) return false;
    if (next.level === level) return true;
  }
  return false;
}

/** The guides before each node of a tree, as the terminal draws them with `│`, `├─`, and `└─`. */
export function guides(items: FigureItem[]): Guide[][] {
  return items.map((node, index) => {
    if (node.level <= 1) return [];
    const columns: Guide[] = [];
    for (let level = 2; level < node.level; level++) {
      let ancestor = index;
      for (let before = index - 1; before >= 0; before--)
        if (items[before]?.level === level) {
          ancestor = before;
          break;
        }
      columns.push(continues(items, ancestor, level) ? "through" : "none");
    }
    columns.push(continues(items, index, node.level) ? "branch" : "last");
    return columns;
  });
}

// compare: a table of options side by side, rows of features against them,
// and the bold option as the pick.

const COMPARE_MAX_ROWS = 30;
const COMPARE_MAX_OPTIONS = 6;
const DIVIDER = /^:?-+:?$/;

/** A table row's cells, without the pipes around them. */
function cells(line: string): string[] {
  let inner = line.startsWith("|") ? line.slice(1) : line;
  if (inner.endsWith("|")) inner = inner.slice(0, -1);
  return inner.split("|").map((cell) => G.trimSpace(G.clean(cell)));
}

function table(body: string): Result<{ heading: string[]; rows: string[][] }> {
  const lines = G.lines(body)
    .map(G.trimSpace)
    .filter((line) => line !== "");
  const loose = lines.find((line) => !line.startsWith("|"));
  if (loose !== undefined)
    return fail(`a compare row is a table row, as in \`| label | a | b |\`, got \`${G.clip(loose)}\``);
  const rows = lines.map(cells);
  const heading = rows[0] ?? [];
  for (let index = 2; index < rows.length; index++) {
    const row = rows[index] ?? [];
    if (row.length !== heading.length)
      return fail(
        `every compare row has ${heading.length} cells, got ${row.length} in \`${G.clip(lines[index] ?? "")}\``,
      );
  }
  const divider = rows[1];
  if (!divider || divider.length === 0 || !divider.every((cell) => DIVIDER.test(cell)))
    return fail("a compare table's second row is its `| --- |` divider");
  return ok({ heading, rows: rows.slice(2) });
}

function compare(info: Info, body: string): Result<Figure> {
  const unknown = G.checkAttributes(info, ["title"]);
  if (unknown) return passOn(unknown);
  if (G.byteLength(body) > G.MAX_BODY) return fail(`the figure is larger than ${G.MAX_BODY / 1024} KiB`);
  const parsed = table(body);
  if (!parsed.ok) return parsed;
  const options = parsed.value.heading.slice(1).map(G.emphasis);
  if (options.length < 1 || options.length > COMPARE_MAX_OPTIONS)
    return fail(`a compare table has 1 to ${COMPARE_MAX_OPTIONS} options, got ${options.length}`);
  if (options.some((each) => each.text === "")) return fail("every compare option needs a heading");
  if (options.filter((each) => each.bold).length > 1) return fail("only one option can be bold");
  const rows = parsed.value.rows;
  if (rows.length === 0) return fail("the figure has no rows");
  if (rows.length > COMPARE_MAX_ROWS) return fail(`a compare table has at most ${COMPARE_MAX_ROWS} rows`);
  const made = rows.map((row) => item({ label: row[0] ?? "", value: "", cells: row.slice(1) }));
  return ok(
    figure("compare", info, made, {
      headings: options.map((each) => each.text),
      pick: options.findIndex((each) => each.bold),
    }),
  );
}

const PARSERS: Record<Kind, (info: Info, body: string) => Result<Figure>> = {
  stat,
  bars,
  meter,
  timeline,
  steps,
  checklist,
  decision,
  delta,
  spec,
  tree,
  compare,
  score,
  stack,
};
