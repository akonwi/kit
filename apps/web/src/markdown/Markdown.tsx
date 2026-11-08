// Renders a Markdown tree as elements. Raw HTML shows as text, links open
// only web and mail addresses, and images are never fetched from elsewhere.
import type { Code, Nodes, PhrasingContent, RootContent, TableRow } from "mdast";
import { createContext, createMemo, For, type JSX, Match, Show, Switch, useContext } from "solid-js";
import { FigureFence } from "../figures/FigureFence";
import { isFigure } from "../figures/figure";
import { Icon, type IconName } from "../ui/icons";
import { CodeBlock } from "./Fence";
import { MermaidFence } from "./MermaidFence";
import {
  type Callout,
  type CalloutKind,
  calloutOf,
  fenceClosed,
  infoString,
  parseMarkdown,
  safeHref,
} from "./parse";

/** The text a tree was parsed from, for the parts drawn from their source. */
const Source = createContext("");

export function Markdown(props: { text: string; class?: string }) {
  const tree = createMemo(() => parseMarkdown(props.text));
  return (
    <Source.Provider value={props.text}>
      <div class={`markdown${props.class ? ` ${props.class}` : ""}`}>
        <Blocks nodes={tree().children} />
      </div>
    </Source.Provider>
  );
}

function Blocks(props: { nodes: readonly RootContent[] }) {
  return <For each={props.nodes}>{(node) => <Block node={node} />}</For>;
}

function Inlines(props: { nodes: readonly Nodes[] }) {
  return <For each={props.nodes}>{(node) => <Inline node={node} />}</For>;
}

function Block(props: { node: RootContent }): JSX.Element {
  const node = props.node;
  switch (node.type) {
    case "paragraph":
      return (
        <p>
          <Inlines nodes={node.children} />
        </p>
      );
    case "heading": {
      const children = <Inlines nodes={node.children} />;
      return (
        <Switch fallback={<h4>{children}</h4>}>
          <Match when={node.depth === 1}>
            <h2>{children}</h2>
          </Match>
          <Match when={node.depth === 2}>
            <h3>{children}</h3>
          </Match>
        </Switch>
      );
    }
    case "blockquote": {
      const callout = calloutOf(node, useContext(Source));
      return callout ? (
        <CalloutCard callout={callout} />
      ) : (
        <blockquote>
          <Blocks nodes={node.children} />
        </blockquote>
      );
    }
    case "list": {
      const items = (
        <For each={node.children}>
          {(item) => (
            <li class={typeof item.checked === "boolean" ? "task" : undefined}>
              <Show when={typeof item.checked === "boolean"}>
                <input
                  type="checkbox"
                  checked={item.checked === true}
                  disabled
                  aria-label={item.checked ? "Done" : "Not done"}
                />
              </Show>
              <Show when={item.spread} fallback={<Tight nodes={item.children} />}>
                <Blocks nodes={item.children} />
              </Show>
            </li>
          )}
        </For>
      );
      return node.ordered ? <ol start={node.start ?? undefined}>{items}</ol> : <ul>{items}</ul>;
    }
    case "code":
      return <Fence node={node} />;
    case "thematicBreak":
      return <hr />;
    case "table":
      return (
        <div class="table-scroll">
          <table>
            <Show when={node.children[0]}>
              {(head) => (
                <thead>
                  <TableRowView row={head()} header align={node.align} />
                </thead>
              )}
            </Show>
            <tbody>
              <For each={node.children.slice(1)}>
                {(row) => <TableRowView row={row} align={node.align} />}
              </For>
            </tbody>
          </table>
        </div>
      );
    case "html":
      return <p class="raw">{node.value}</p>;
    case "definition":
    case "footnoteDefinition":
      return null;
    default:
      return (
        <p>
          <Inline node={node as Nodes} />
        </p>
      );
  }
}

/** A tight list item's paragraphs render without paragraph spacing. */
function Tight(props: { nodes: readonly RootContent[] }) {
  return (
    <For each={props.nodes}>
      {(node) => (node.type === "paragraph" ? <Inlines nodes={node.children} /> : <Block node={node} />)}
    </For>
  );
}

function TableRowView(props: { row: TableRow; header?: boolean; align?: (string | null)[] | null }) {
  return (
    <tr>
      <For each={props.row.children}>
        {(cell, index) => {
          const align = props.align?.[index()] ?? undefined;
          const content = <Inlines nodes={cell.children} />;
          return props.header ? <th data-align={align}>{content}</th> : <td data-align={align}>{content}</td>;
        }}
      </For>
    </tr>
  );
}

/** A fenced block in a reply. */
function Fence(props: { node: Code }) {
  const source = useContext(Source);
  if ((props.node.lang ?? "").toLowerCase() === "mermaid")
    return <MermaidFence code={props.node.value} closed={fenceClosed(props.node, source)} />;
  const info = infoString(props.node);
  if (isFigure(info))
    return <FigureFence info={info} body={props.node.value} closed={fenceClosed(props.node, source)} />;
  return <CodeBlock code={props.node.value} lang={props.node.lang} />;
}

const CALLOUT_ICONS: Record<CalloutKind, IconName> = {
  note: "info",
  tip: "lightbulb",
  important: "alertCircle",
  warning: "alert",
  caution: "alertOctagon",
};

/**
 * A GitHub alert as the Mac draws it: a card tinted with its kind's color,
 * the kind's icon and name, the title after a dot, then the body. No label
 * bar or Copy, so it never reads as code or a figure.
 */
function CalloutCard(props: { callout: Callout }) {
  const label = () => props.callout.kind[0]?.toUpperCase() + props.callout.kind.slice(1);
  return (
    <aside class="callout" data-kind={props.callout.kind}>
      <p class="callout-head">
        <Icon name={CALLOUT_ICONS[props.callout.kind]} size={14} />
        <span>
          {label()}
          <Show when={props.callout.title}>
            <span class="callout-title"> · {props.callout.title}</span>
          </Show>
        </span>
      </p>
      <Show when={props.callout.blocks.length > 0}>
        <div class="callout-body">
          <Blocks nodes={props.callout.blocks} />
        </div>
      </Show>
    </aside>
  );
}

function Inline(props: { node: Nodes }): JSX.Element {
  const node = props.node;
  switch (node.type) {
    case "text":
      return <>{node.value}</>;
    case "strong":
      return (
        <strong>
          <Inlines nodes={node.children} />
        </strong>
      );
    case "emphasis":
      return (
        <em>
          <Inlines nodes={node.children} />
        </em>
      );
    case "delete":
      return (
        <del>
          <Inlines nodes={node.children} />
        </del>
      );
    case "inlineCode":
      return <code>{node.value}</code>;
    case "break":
      return <br />;
    case "link": {
      const href = safeHref(node.url);
      const children = <Inlines nodes={node.children as PhrasingContent[]} />;
      return href ? (
        <a href={href} target="_blank" rel="noopener noreferrer">
          {children}
        </a>
      ) : (
        <span>{children}</span>
      );
    }
    case "image":
      // Images from elsewhere are never fetched; the address is shown instead.
      return <span class="image-ref">{node.alt || node.url}</span>;
    case "html":
      return <>{node.value}</>;
    case "linkReference":
    case "imageReference":
      return <>{"children" in node ? <Inlines nodes={node.children as PhrasingContent[]} /> : null}</>;
    case "footnoteReference":
      return <sup>{node.identifier}</sup>;
    default:
      if ("children" in node) return <Inlines nodes={node.children as Nodes[]} />;
      if ("value" in node) return <>{String(node.value)}</>;
      return null;
  }
}
