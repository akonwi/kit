// Fenced content in a card, as the Mac draws code, figures, and diagrams.
import { createMemo, createSignal, type JSX, onCleanup, Show, useContext } from "solid-js";
import { copyText } from "../ui/copy";
import { Icon } from "../ui/icons";
import { coloredLines, HighlightContext, type Run } from "./highlight";

/**
 * A card for fenced content, as the Mac draws code, figures, and diagrams:
 * a label, Copy for the fenced source, any other actions, then the body.
 */
export function FenceCard(props: {
  label: string;
  copy: string;
  actions?: JSX.Element;
  class?: string;
  children: JSX.Element;
}) {
  const [copied, setCopied] = createSignal(false);
  let reset: ReturnType<typeof setTimeout> | undefined;
  onCleanup(() => clearTimeout(reset));
  const copy = async () => {
    if (!(await copyText(props.copy))) return;
    setCopied(true);
    clearTimeout(reset);
    reset = setTimeout(() => setCopied(false), 1500);
  };
  return (
    <figure class={props.class ? `fence ${props.class}` : "fence"}>
      <figcaption>
        <span class="fence-label">{props.label}</span>
        <button type="button" class="fence-action" onClick={copy}>
          <Icon name={copied() ? "check" : "copy"} size={12} />
          {copied() ? "Copied" : "Copy"}
        </button>
        {props.actions}
      </figcaption>
      {props.children}
    </figure>
  );
}

/**
 * Code in a fence card under its language, or `code` when it names none. A
 * labelled block takes the page highlighter's colors when they arrive, in
 * place of the same plain text.
 */
export function CodeBlock(props: { code: string; lang?: string | null; title?: string }) {
  const highlighter = useContext(HighlightContext);
  const lines = createMemo(() => {
    const colors = props.lang && highlighter ? highlighter.colors(props.lang, props.code)() : undefined;
    return colors ? coloredLines(props.code, colors) : undefined;
  });
  return (
    <FenceCard label={props.title ?? props.lang ?? "code"} copy={props.code} class="code-block">
      <pre>
        <Show when={lines()} fallback={<code>{props.code}</code>}>
          {(colored) => (
            <code>{colored().map((runs, index) => line(runs, index, colored().length, props.code))}</code>
          )}
        </Show>
      </pre>
    </FenceCard>
  );
}

/** One colored line, ended as the source ends it. */
function line(runs: Run[], index: number, count: number, source: string): JSX.Element {
  const ending = index < count - 1 || source.endsWith("\n") ? "\n" : "";
  return (
    <>
      {runs.map((run) => (run.tone ? <span class={`hl-${run.tone}`}>{run.text}</span> : run.text))}
      {ending}
    </>
  );
}
