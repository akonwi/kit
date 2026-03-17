import { For, Show, onCleanup } from "solid-js";
import type { PagerController } from "../features/pager";
import { syntaxStyle, theme } from "./theme";

export type PagerViewProps = {
  pager: PagerController;
};

export function PagerView(props: PagerViewProps) {
  const pager = props.pager;

  let scrollRef: any;

  // Bind scroll delegate so the pager controller can scroll via keyboard
  function bindScroll(ref: any) {
    scrollRef = ref;
    pager.setScrollDelegate({
      scrollBy: (delta: number) => scrollRef?.scrollBy({ x: 0, y: delta }),
    });
  }

  onCleanup(() => pager.setScrollDelegate(null));

  return (
    <box flexGrow={1} height="100%" flexDirection="column">
      {/* Scrollable section content */}
      <scrollbox
        ref={bindScroll}
        flexGrow={1}
        scrollY
        stickyStart="top"
        padding={1}
        style={{
          scrollbarOptions: {
            trackOptions: {
              foregroundColor: theme.scrollbarFg,
              backgroundColor: theme.scrollbarBg,
            },
          },
        }}
      >
        <box flexDirection="column" gap={0} width="100%">
          {/* Persistent section title (parent heading) */}
          <Show when={pager.currentSection?.sectionTitle}>
            <text fg={theme.textMuted}>
              <b>{pager.currentSection!.sectionTitle}</b>
            </text>
          </Show>

          {/* Section body */}
          <code
            filetype="markdown"
            content={pager.currentSection?.body ?? ""}
            syntaxStyle={syntaxStyle}
            conceal
            drawUnstyledText={false}
            fg={theme.textPrimary}
          />
        </box>
      </scrollbox>

      {/* Fixed footer: dots + hints */}
      <box flexShrink={0} flexDirection="column" paddingX={1} paddingBottom={0}>
        {/* Section dots + position */}
        <box flexDirection="row" gap={1}>
          <For each={pager.sections}>
            {(_, idx) => {
              const isCurrent = () => idx() === pager.currentIndex;
              const hasNote = () => pager.notes.has(idx());
              const color = () => {
                if (isCurrent()) return hasNote() ? theme.borderAccent : theme.textPrimary;
                return hasNote() ? theme.toolText : theme.textMuted;
              };
              return (
                <text fg={color()}>
                  {isCurrent() ? (hasNote() ? "◆" : "●") : (hasNote() ? "●" : "○")}
                </text>
              );
            }}
          </For>
          <text fg={theme.textMuted}>
            {pager.currentIndex + 1}/{pager.sections.length}
            {pager.getNoteCount() > 0
              ? ` · ${pager.getNoteCount()} note${pager.getNoteCount() === 1 ? "" : "s"}`
              : ""}
          </text>
        </box>
        {/* Navigation hints */}
        <text fg={theme.textMuted}>
          Ctrl+Shift+←/→ section · Escape close · Ctrl+Enter submit notes
        </text>
      </box>
    </box>
  );
}
