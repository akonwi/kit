// A fenced figure (ADR 0054), drawn as the web design and the Mac draw it: a
// fence card labelled with the figure's title, or its kind, with Copy for
// the fenced source, holding the figure in the interface font with tabular
// figures. One accent marks the bold item; everything else is text, muted,
// or border. A figure still streaming shows its source, and one that isn't
// drawn shows its source with the reason.
import { createMemo, For, type JSX, Match, Show, Switch } from "solid-js";
import { CodeBlock, FenceCard } from "../markdown/Fence";
import { Icon } from "../ui/icons";
import { cut, type Figure, type FigureItem, fenced, type Guide, guides, parseFigure } from "./figure";
import { name } from "./grammar";

export function FigureFence(props: { info: string; body: string; closed: boolean }) {
  const kind = () => name(props.info);
  const parsed = createMemo(() => (props.closed ? parseFigure(props.info, props.body) : undefined));
  return (
    <Show when={parsed()} fallback={<CodeBlock code={props.body} title={kind()} />}>
      {(result) => {
        const value = result();
        return value.ok ? (
          <FenceCard
            label={value.value.title || value.value.kind}
            copy={fenced(props.info, props.body)}
            class="figure"
          >
            <div class="figure-body" data-kind={value.value.kind}>
              <FigureView figure={value.value} />
            </div>
          </FenceCard>
        ) : (
          <div class="undrawn">
            <CodeBlock code={props.body} title={kind()} />
            <p class="undrawn-note">Figure not drawn: {value.error.reason}</p>
          </div>
        );
      }}
    </Show>
  );
}

/** A figure's body, drawn for its kind. */
export function FigureView(props: { figure: Figure }): JSX.Element {
  const figure = props.figure;
  switch (figure.kind) {
    case "stat":
      return <StatFigure items={figure.items} />;
    case "bars":
      return <BarsFigure items={figure.items} />;
    case "meter":
      return <MeterFigure items={figure.items} />;
    case "timeline":
      return <RailFigure items={figure.items} lane />;
    case "steps":
      return <RailFigure items={figure.items} lane={false} />;
    case "checklist":
      return <ChecklistFigure items={figure.items} />;
    case "decision":
      return <DecisionFigure figure={figure} />;
    case "delta":
      return <DeltaFigure figure={figure} />;
    case "spec":
      return <SpecFigure items={figure.items} />;
    case "tree":
      return <TreeFigure items={figure.items} />;
    case "compare":
      return <CompareFigure figure={figure} />;
    case "score":
      return <ScoreFigure items={figure.items} />;
    case "stack":
      return <StackFigure figure={figure} />;
  }
}

/** How a row reads: the accent, receding, or plain. */
function tone(item: FigureItem, emphasized: boolean): "accent" | "quiet" | "plain" {
  if (item.accent) return "accent";
  return item.muted || emphasized ? "quiet" : "plain";
}

const anyAccent = (items: FigureItem[]) => items.some((each) => each.accent);
const anyNote = (items: FigureItem[]) => items.some((each) => each.note !== "");

/** A note and body under a title, muted. */
function Under(props: { item: FigureItem }) {
  return (
    <>
      <Show when={props.item.note}>
        <span class="fig-sub">{props.item.note}</span>
      </Show>
      <Show when={props.item.body}>
        <span class="fig-sub">{props.item.body}</span>
      </Show>
    </>
  );
}

// stat

function StatFigure(props: { items: FigureItem[] }) {
  return (
    <div class="fig-stat">
      <For each={props.items}>
        {(item) => (
          <div class="fig-stat-item" data-tone={item.accent ? "accent" : item.muted ? "quiet" : "plain"}>
            <span class="fig-stat-value">{item.value}</span>
            <span class="fig-sub">{item.label}</span>
            <Show when={item.spark.length > 1}>
              <Sparkline points={item.spark} />
            </Show>
            <Show when={item.note}>
              <span class="fig-sub fig-stat-note">{item.note}</span>
            </Show>
          </div>
        )}
      </For>
    </div>
  );
}

/** A stat's sparkline: a muted line, its last point in the accent. */
function Sparkline(props: { points: number[] }) {
  const height = 24;
  const width = () => Math.min(120, Math.max(60, props.points.length * 8));
  const at = (index: number) => {
    const step = (width() - 6) / (props.points.length - 1);
    return [3 + step * index, height - 3 - ((props.points[index] ?? 0) / 1000) * (height - 6)] as const;
  };
  const line = () => props.points.map((_, index) => at(index).join(",")).join(" ");
  const last = () => at(props.points.length - 1);
  return (
    <svg
      class="fig-spark"
      width={width()}
      height={height}
      viewBox={`0 0 ${width()} ${height}`}
      aria-hidden="true"
    >
      <polyline points={line()} />
      <circle cx={last()[0]} cy={last()[1]} r="2.5" />
    </svg>
  );
}

// bars, meter, score, stack: a label lane, a drawing, then values.

/** A 6px track filled scale thousandths of its width, with an optional tick. */
function Track(props: { scale: number; fill: string; tick?: number }) {
  return (
    <span class="fig-track" aria-hidden="true">
      <Show when={props.scale > 0}>
        <span
          class="fig-fill"
          data-fill={props.fill}
          style={{ "inline-size": `max(6px, ${props.scale / 10}%)` }}
        />
      </Show>
      <Show when={props.tick !== undefined}>
        <span class="fig-tick" style={{ "inset-inline-start": `${(props.tick ?? 0) * 100}%` }} />
      </Show>
    </span>
  );
}

/** A row's label: its full text on hover, cut with an ellipsis when long. */
function Label(props: { item: FigureItem; tone: string }) {
  return (
    <span class="fig-label" data-tone={props.tone} title={props.item.label}>
      {props.item.label}
    </span>
  );
}

function BarsFigure(props: { items: FigureItem[] }) {
  const emphasized = anyAccent(props.items);
  const shares = props.items.some((each) => each.share !== "");
  const notes = anyNote(props.items);
  const columns = [
    "fit-content(40%)",
    "minmax(4rem, 1fr)",
    "auto",
    shares && "auto",
    notes && "minmax(0, auto)",
  ];
  return (
    <div class="fig-grid fig-rows" style={{ "grid-template-columns": columns.filter(Boolean).join(" ") }}>
      <For each={props.items}>
        {(item) => {
          const look = tone(item, emphasized);
          return (
            <>
              <Label item={item} tone={look} />
              <Track scale={item.scale} fill={look === "plain" ? "muted" : look} />
              <span class="fig-value" data-tone={look} title={item.value}>
                {item.value}
              </span>
              <Show when={shares}>
                <span class="fig-value fig-quiet">{item.share}</span>
              </Show>
              <Show when={notes}>
                <span class="fig-note">{item.note}</span>
              </Show>
            </>
          );
        }}
      </For>
    </div>
  );
}

function MeterFigure(props: { items: FigureItem[] }) {
  const single = () => {
    const first = props.items[0];
    return props.items.length === 1 && first && first.target === "" ? first : undefined;
  };
  return (
    <Show when={single()} fallback={<MeterRows items={props.items} />}>
      {(item) => (
        <div class="fig-meter">
          <div class="fig-meter-line">
            <Track scale={item().scale} fill={item().over ? "warning" : "accent"} />
            <span class="fig-meter-value" data-over={item().over || undefined} title={item().value}>
              {item().value}
            </span>
          </div>
          <Show when={item().note}>
            <span class="fig-sub">{item().note}</span>
          </Show>
        </div>
      )}
    </Show>
  );
}

function MeterRows(props: { items: FigureItem[] }) {
  const emphasized = anyAccent(props.items);
  const notes = anyNote(props.items);
  const columns = ["fit-content(40%)", "minmax(4rem, 1fr)", "auto", notes && "minmax(0, auto)"];
  return (
    <div
      class="fig-grid fig-rows fig-meter-rows"
      style={{ "grid-template-columns": columns.filter(Boolean).join(" ") }}
    >
      <For each={props.items}>
        {(item) => {
          const look = tone(item, emphasized);
          const fill = item.over ? "warning" : look === "plain" ? "muted" : look;
          return (
            <>
              <Label item={item} tone={item.accent ? "accent" : item.muted ? "quiet" : "plain"} />
              <Track scale={item.scale} fill={fill} tick={0.8} />
              <span
                class="fig-value"
                data-tone={look}
                data-over={item.over || undefined}
                title={`${item.value} / ${item.target}`}
              >
                {item.value} / {item.target}
              </span>
              <Show when={notes}>
                <span class="fig-note">{item.note}</span>
              </Show>
            </>
          );
        }}
      </For>
    </div>
  );
}

function ScoreFigure(props: { items: FigureItem[] }) {
  const emphasized = anyAccent(props.items);
  const notes = anyNote(props.items);
  const columns = ["fit-content(40%)", "auto", "auto", notes && "minmax(0, 1fr)"];
  return (
    <div
      class="fig-grid fig-rows fig-score"
      style={{ "grid-template-columns": columns.filter(Boolean).join(" ") }}
    >
      <For each={props.items}>
        {(item) => {
          const look = tone(item, emphasized);
          const fill = look === "plain" ? "muted" : look;
          return (
            <>
              <Label item={item} tone={look} />
              <span class="fig-dots" aria-hidden="true">
                <For each={Array.from({ length: item.slots }, (_, slot) => slot)}>
                  {(slot) => (
                    <span
                      class="fig-dot"
                      data-fill={fill}
                      data-halves={Math.min(Math.max(item.filled - slot * 2, 0), 2)}
                    />
                  )}
                </For>
              </span>
              <span class="fig-value fig-start" data-tone={look} title={item.value}>
                {item.value}
              </span>
              <Show when={notes}>
                <span class="fig-note">{item.note}</span>
              </Show>
            </>
          );
        }}
      </For>
    </div>
  );
}

/** A stack row's segments as a screen reader hears them: each name and its share. */
function shares(item: FigureItem, headings: string[]): string {
  return item.parts.map((part, index) => `${headings[index] ?? ""} ${Math.round(part / 10)}%`).join(", ");
}

/** The opacity of each of a stack's segments, in order. */
const SHADES = [1, 0.65, 0.4, 0.25];

function StackFigure(props: { figure: Figure }) {
  const items = props.figure.items;
  const emphasized = anyAccent(items);
  const notes = anyNote(items);
  const columns = ["fit-content(40%)", "minmax(6rem, 1fr)", "auto", notes && "minmax(0, auto)"];
  return (
    <div class="fig-stack">
      <div class="fig-grid fig-rows" style={{ "grid-template-columns": columns.filter(Boolean).join(" ") }}>
        <For each={items}>
          {(item) => {
            const look = tone(item, emphasized);
            const fill = look === "plain" ? "muted" : look;
            return (
              <>
                <Label item={item} tone={item.accent ? "accent" : item.muted ? "quiet" : "plain"} />
                <span class="fig-segments" aria-hidden="true">
                  <Show when={item.scale > 0}>
                    <span class="fig-span" style={{ "flex-basis": `calc(${item.scale / 10}% - 1px)` }}>
                      <For each={item.parts}>
                        {(part, index) => (
                          <Show when={part > 0}>
                            <span
                              class="fig-segment"
                              data-fill={fill}
                              style={{ "flex-grow": part, opacity: SHADES[Math.min(index(), 3)] }}
                            />
                          </Show>
                        )}
                      </For>
                    </span>
                  </Show>
                  <Show when={item.scale < 1000}>
                    <span class="fig-rest" />
                  </Show>
                </span>
                <span class="fig-value" data-tone={look} title={item.value}>
                  {item.value}
                  <span class="visually-hidden">{` (${shares(item, props.figure.headings)})`}</span>
                </span>
                <Show when={notes}>
                  <span class="fig-note">{item.note}</span>
                </Show>
              </>
            );
          }}
        </For>
      </div>
      <div class="fig-legend" aria-hidden="true">
        <For each={props.figure.headings}>
          {(heading, index) => (
            <span class="fig-sub">
              <span class="fig-swatch" style={{ opacity: SHADES[Math.min(index(), 3)] }} aria-hidden="true" />
              {heading}
            </span>
          )}
        </For>
      </div>
    </div>
  );
}

// timeline and steps: markers on a rail.

/** A timeline's dot: filled when done or when nothing is bold, the accent when now, hollow after. */
function TimelineMarker(props: { item: FigureItem }) {
  return <span class="fig-dot-marker" data-state={props.item.state || "none"} />;
}

/** A step's circle: a check when done, otherwise its number, filled with the accent when current. */
function StepMarker(props: { item: FigureItem }) {
  return (
    <span class="fig-step-marker" data-state={props.item.state || "none"}>
      <Show when={props.item.state === "done"} fallback={props.item.label}>
        <Icon name="check" size={10} />
      </Show>
    </span>
  );
}

function RailFigure(props: { items: FigureItem[]; lane: boolean }) {
  return (
    <div class="fig-rail" data-lane={props.lane || undefined}>
      <For each={props.items}>
        {(item, index) => (
          <>
            <span class="fig-rail-mark" aria-hidden="true">
              {props.lane ? <TimelineMarker item={item} /> : <StepMarker item={item} />}
              <Show when={index() < props.items.length - 1}>
                <span class="fig-rail-line" />
              </Show>
            </span>
            <Show when={props.lane}>
              <span class="fig-lane" data-state={item.state || undefined} title={item.label}>
                {item.label}
              </span>
            </Show>
            <span
              class="fig-rail-text"
              data-tone={
                item.accent
                  ? "accent"
                  : item.muted || (!props.lane && item.state === "done")
                    ? "quiet"
                    : "plain"
              }
            >
              <span class="fig-title">
                <span class="visually-hidden">{spokenState(item, props.lane)}</span>
                {item.value}
              </span>
              <Under item={item} />
            </span>
          </>
        )}
      </For>
    </div>
  );
}

/** What a rail's marker shows, for a screen reader: a step's number and where an item stands. */
function spokenState(item: FigureItem, lane: boolean): string {
  const standing = item.state === "done" ? "done" : item.state === "now" ? (lane ? "now" : "current") : "";
  if (lane) return standing ? `${standing[0]?.toUpperCase()}${standing.slice(1)}: ` : "";
  return `Step ${item.label}${standing ? `, ${standing}` : ""}: `;
}

// checklist

function ChecklistFigure(props: { items: FigureItem[] }) {
  return (
    <ul class="fig-checklist">
      <For each={props.items}>
        {(item) => (
          <li data-level={item.level}>
            <span
              class="fig-box"
              data-state={item.state}
              role="img"
              aria-label={item.state === "done" ? "Done" : "Not done"}
            >
              <Show when={item.state === "done"}>
                <Icon name="check" size={10} />
              </Show>
            </span>
            <span
              class="fig-rail-text"
              data-tone={item.accent ? "accent" : item.muted || item.state === "done" ? "quiet" : "plain"}
            >
              <span class="fig-title">{item.value}</span>
              <Under item={item} />
            </span>
          </li>
        )}
      </For>
    </ul>
  );
}

// decision

function DecisionFigure(props: { figure: Figure }) {
  const status = props.figure.status;
  return (
    <div class="fig-decision">
      <Show when={status || props.figure.date}>
        <div class="fig-decision-head">
          <Show when={status}>
            <span class="fig-chip" data-status={status}>
              {status.charAt(0).toUpperCase() + status.slice(1)}
            </span>
          </Show>
          <Show when={props.figure.date}>
            <span class="fig-sub">{cut(props.figure.date, 20)}</span>
          </Show>
        </div>
      </Show>
      <ul class="fig-options">
        <For each={props.figure.items}>
          {(item) => (
            <li>
              <span
                class="fig-option-marker"
                data-tone={item.accent ? "accent" : item.muted ? "quiet" : "plain"}
              >
                <Show when={!item.accent && item.muted}>×</Show>
              </span>
              <span class="fig-rail-text" data-tone={item.accent ? "accent" : item.muted ? "quiet" : "plain"}>
                <span class="fig-title">{item.label}</span>
                <Show when={item.note}>
                  <span class="fig-sub">{item.note}</span>
                </Show>
              </span>
            </li>
          )}
        </For>
      </ul>
      <Show when={props.figure.note}>
        <p class="fig-decision-note">{props.figure.note}</p>
      </Show>
    </div>
  );
}

// delta

function DeltaFigure(props: { figure: Figure }) {
  const items = props.figure.items;
  const paired = items.some((each) => each.from !== "");
  const signs = items.some((each) => each.sign !== "");
  const changes = items.some((each) => each.change !== "");
  const notes = anyNote(items);
  const columns = [
    "minmax(0, 1fr)",
    paired && "auto auto",
    !paired && signs && "auto",
    "auto",
    changes && "auto",
    notes && "minmax(0, auto)",
  ];
  const [from, to] = props.figure.headings;
  return (
    <div class="fig-grid fig-delta" style={{ "grid-template-columns": columns.filter(Boolean).join(" ") }}>
      <Show when={props.figure.headings.length === 2}>
        <span />
        <span class="fig-heading">{from}</span>
        <span />
        <span class="fig-heading">{to}</span>
        <Show when={changes}>
          <span />
        </Show>
        <Show when={notes}>
          <span />
        </Show>
      </Show>
      <For each={items}>
        {(item) => {
          const base = item.muted ? "quiet" : "plain";
          const trend = item.trend || base;
          return (
            <>
              <Show when={item.accent && items.length > 1}>
                <span class="fig-rule" />
              </Show>
              <span class="fig-label" data-tone={item.accent ? "accent" : base} title={item.label}>
                {item.label}
              </span>
              <Show when={paired}>
                <span class="fig-value fig-quiet" title={item.from}>
                  {item.from}
                </span>
                <span class="fig-arrow" aria-hidden="true">
                  →
                </span>
              </Show>
              <Show when={!paired && signs}>
                <span class="fig-value fig-sign" data-tone={trend} data-bold={item.accent || undefined}>
                  {item.sign}
                </span>
              </Show>
              <span
                class="fig-value"
                data-tone={item.accent ? "pick" : paired ? base : trend}
                title={item.value}
              >
                {item.value}
              </span>
              <Show when={changes}>
                <span class="fig-value" data-tone={trend} data-bold>
                  {item.change}
                </span>
              </Show>
              <Show when={notes}>
                <span class="fig-note">{item.note}</span>
              </Show>
            </>
          );
        }}
      </For>
    </div>
  );
}

// spec

function SpecFigure(props: { items: FigureItem[] }) {
  return (
    <dl class="fig-grid fig-spec">
      <For each={props.items}>
        {(item) => (
          <>
            <dt class="fig-label" data-tone={item.accent ? "accent" : "quiet"} title={item.label}>
              {item.label}
            </dt>
            <dd class="fig-rail-text" data-tone={item.accent ? "accent" : item.muted ? "quiet" : "plain"}>
              <span class="fig-title">{item.value}</span>
              <Under item={item} />
            </dd>
          </>
        )}
      </For>
    </dl>
  );
}

// tree

function TreeFigure(props: { items: FigureItem[] }) {
  const columns = guides(props.items);
  const notes = anyNote(props.items);
  return (
    <div class="fig-tree" style={{ "grid-template-columns": notes ? "auto minmax(0, 1fr)" : "auto" }}>
      <For each={props.items}>
        {(item, index) => (
          <>
            <span class="fig-node">
              <For each={columns[index()] ?? []}>
                {(guide: Guide) => <span class="fig-guide" data-guide={guide} />}
              </For>
              <span
                class="fig-node-name"
                data-tone={item.accent ? "accent" : item.muted ? "quiet" : "plain"}
                title={item.label}
              >
                {cut(item.label)}
              </span>
            </span>
            <Show when={notes}>
              <span class="fig-note">{item.note}</span>
            </Show>
          </>
        )}
      </For>
    </div>
  );
}

// compare: the criteria stay put while the options scroll sideways.

function CompareCell(props: { text: string }) {
  return (
    <Switch fallback={<span class="fig-cell">{cut(props.text)}</span>}>
      <Match when={props.text === "yes"}>
        <span class="fig-cell fig-yes" role="img" aria-label="yes">
          <Icon name="check" size={12} />
        </span>
      </Match>
      <Match when={props.text === "no"}>
        <span class="fig-cell fig-no" role="img" aria-label="no">
          –
        </span>
      </Match>
    </Switch>
  );
}

function CompareFigure(props: { figure: Figure }) {
  return (
    <div class="fig-compare">
      <div class="fig-criteria">
        <span class="fig-cell" />
        <For each={props.figure.items}>
          {(item) => (
            <span class="fig-cell" title={item.label}>
              {cut(item.label)}
            </span>
          )}
        </For>
      </div>
      <div class="fig-options-scroll">
        <For each={props.figure.headings}>
          {(heading, column) => {
            const picked = column() === props.figure.pick;
            return (
              <div class="fig-option" data-pick={picked || undefined}>
                <span class="fig-cell fig-option-heading">{cut(heading)}</span>
                <For each={props.figure.items}>
                  {(item) => <CompareCell text={item.cells[column()] ?? ""} />}
                </For>
              </div>
            );
          }}
        </For>
      </div>
    </div>
  );
}
