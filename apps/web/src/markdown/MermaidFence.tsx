// A Mermaid fence: its source while the reply streams, then the diagram Pele
// lays out to the card's width, with an expand button for full screen.
// Anything Pele won't draw shows its source with the reason.
import { createEffect, createResource, createSignal, Match, on, onCleanup, Show, Switch } from "solid-js";
import { copyText } from "../ui/copy";
import { Icon } from "../ui/icons";
import { Viewer } from "../ui/Viewer";
import { diagramNode, drawDiagram, loadPele, notDrawnReason, renderOptions } from "./diagram";
import { CodeBlock, FenceCard } from "./Fence";

let drawn = 0;
/** A prefix for one drawing's ids, so two drawings' markers never collide. */
const idPrefix = () => `kd${++drawn}-`;

/** The font Pele measures and sets labels in: the page's interface font. */
function bodyFont(): string {
  return getComputedStyle(document.documentElement).getPropertyValue("--font-body").trim() || "system-ui";
}

export function MermaidFence(props: { code: string; closed: boolean }) {
  return (
    <Show when={props.closed} fallback={<CodeBlock code={props.code} lang="mermaid" />}>
      <MermaidDiagram source={props.code} />
    </Show>
  );
}

function MermaidDiagram(props: { source: string }) {
  const [pele] = createResource(loadPele);
  const [failure, setFailure] = createSignal<string>();
  const [kind, setKind] = createSignal("");
  const [open, setOpen] = createSignal(false);
  const reason = () => failure() ?? (pele.error ? "the diagram renderer didn’t load" : undefined);

  // Draws into the card's body at its width, again whenever the width changes.
  const draw = (body: HTMLDivElement) => {
    const prefix = idPrefix();
    const [width, setWidth] = createSignal(0);
    // Drawing changes the body's height, so a width is taken up on the next
    // frame rather than inside the observer's own notification.
    let frame = 0;
    const observer = new ResizeObserver(([entry]) => {
      const next = Math.floor(entry?.contentRect.width ?? 0);
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => setWidth(next));
    });
    observer.observe(body);
    onCleanup(() => {
      cancelAnimationFrame(frame);
      observer.disconnect();
    });
    let last = "";
    createEffect(
      on([pele, width, () => props.source], ([engine, maxWidth, source]) => {
        if (!engine || maxWidth <= 0) return;
        try {
          const result = drawDiagram(engine, source, {
            ...renderOptions(bodyFont()),
            maxWidth,
            idPrefix: prefix,
          });
          setKind(result.type);
          if (result.svg === last) return;
          last = result.svg;
          body.replaceChildren(diagramNode(result.svg));
        } catch (error) {
          setFailure(notDrawnReason(error, source));
        }
      }),
    );
  };

  return (
    <Switch>
      <Match when={reason()}>
        {(text) => (
          <div class="undrawn">
            <CodeBlock code={props.source} lang="mermaid" />
            <p class="undrawn-note">Diagram not drawn: {text()}</p>
          </div>
        )}
      </Match>
      <Match when={pele()}>
        <FenceCard
          label="mermaid"
          copy={props.source}
          class="diagram"
          actions={
            <button
              type="button"
              class="fence-action"
              aria-label="Open the diagram full screen"
              onClick={() => setOpen(true)}
            >
              <Icon name="expand" size={14} />
            </button>
          }
        >
          <div class="diagram-body pele-theme" ref={draw} />
        </FenceCard>
        <DiagramViewer source={props.source} kind={kind()} open={open()} onClose={() => setOpen(false)} />
      </Match>
      <Match when={true}>
        <CodeBlock code={props.source} lang="mermaid" />
      </Match>
    </Switch>
  );
}

/** A diagram over the session at its own layout, to pan and zoom. */
function DiagramViewer(props: { source: string; kind: string; open: boolean; onClose(): void }) {
  const [copied, setCopied] = createSignal(false);
  let reset: ReturnType<typeof setTimeout> | undefined;
  onCleanup(() => clearTimeout(reset));
  const copy = async () => {
    if (!(await copyText(props.source))) return;
    setCopied(true);
    clearTimeout(reset);
    reset = setTimeout(() => setCopied(false), 1500);
  };

  // Drawn at the layout it takes with room to spare.
  const stage = (host: HTMLDivElement) => {
    let zoom: { destroy(): void } | undefined;
    let gone = false;
    onCleanup(() => {
      gone = true;
      zoom?.destroy();
    });
    loadPele()
      .then((pele) => {
        if (gone) return;
        const result = drawDiagram(pele, props.source, {
          ...renderOptions(bodyFont()),
          idPrefix: idPrefix(),
        });
        host.replaceChildren(diagramNode(result.svg));
        const zooming = pele.enableZoom(host, { always: true, controls: false, maxScale: 4 });
        zoom = zooming;
        // A diagram wider than the screen opens at full size, to pan; a
        // pinch out still fits it whole.
        if (zooming.fit < 1) zooming.zoomTo(1);
      })
      .catch(() => host.replaceChildren());
  };

  return (
    <Viewer open={props.open} label="Diagram" onClose={() => props.onClose()}>
      {(close) => (
        <>
          <header class="viewer-bar">
            <button type="button" class="icon-button" aria-label="Close" onClick={close}>
              <Icon name="close" />
            </button>
            <div class="viewer-title">
              <h2>Diagram</h2>
              <p class="mono muted">{props.kind ? `mermaid · ${props.kind}` : "mermaid"}</p>
            </div>
            <button type="button" class="icon-button" aria-label="Copy the source" onClick={copy}>
              <Icon name={copied() ? "check" : "copy"} />
            </button>
          </header>
          <div class="viewer-stage" data-kind="diagram">
            <div class="diagram-stage pele-theme" ref={stage} />
            <p class="diagram-hint">Drag to pan · pinch to zoom</p>
          </div>
        </>
      )}
    </Viewer>
  );
}
