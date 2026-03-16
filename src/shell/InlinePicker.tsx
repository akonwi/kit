import type { KeyEvent } from "@opentui/core";
import { createMemo, For, Show } from "solid-js";
import type { PaletteManager } from "../state/palette-manager";
import { theme } from "./theme";

const MAX_VISIBLE = 10;

export type InlinePickerProps = {
  palette: PaletteManager;
};

function computeScrollbar(total: number, visible: number, offset: number) {
  if (total <= visible) return null;
  const thumbSize = Math.max(1, Math.round((visible / total) * visible));
  const maxOffset = total - visible;
  const thumbOffset = Math.round((offset / maxOffset) * (visible - thumbSize));
  const track: boolean[] = [];
  for (let i = 0; i < visible; i++) {
    track.push(i >= thumbOffset && i < thumbOffset + thumbSize);
  }
  return track;
}

export function InlinePicker(props: InlinePickerProps) {
  const palette = () => props.palette.current();

  const maxNameLen = () =>
    palette().options.reduce((max, o) => Math.max(max, o.name.length), 0);

  const visibleSlice = createMemo(() => {
    const p = palette();
    const options = p.options;
    const count = options.length;
    const selected = p.selectedIndex;

    if (count <= MAX_VISIBLE) {
      return {
        items: options.map((o, i) => ({ option: o, index: i })),
        offset: 0,
      };
    }

    let offset = selected - Math.floor(MAX_VISIBLE / 2);
    offset = Math.max(0, Math.min(offset, count - MAX_VISIBLE));

    const items = options
      .slice(offset, offset + MAX_VISIBLE)
      .map((o, i) => ({ option: o, index: offset + i }));

    return { items, offset };
  });

  const scrollbar = createMemo(() =>
    computeScrollbar(
      palette().options.length,
      MAX_VISIBLE,
      visibleSlice().offset,
    ),
  );

  return (
    <Show when={palette().visible}>
      <box
        position="absolute"
        bottom={12}
        left={0}
        width="100%"
        border
        borderColor={theme.pickerBorder}
        backgroundColor={theme.pickerBg}
        paddingX={1}
        flexDirection="column"
      >
        <Show when={palette().mode === "input"}>
          <text fg={theme.textMuted}>{palette().label}</text>
          <box flexDirection="row" gap={1} width="100%">
            <text flexBasis={1} fg={theme.textPrimary}>
              {">"}
            </text>
            <input
              flexGrow={1}
              focused
              value={palette().inputValue}
              onInput={(value: string) => props.palette.setInputValue(value)}
              onKeyDown={(e: KeyEvent) => {
                if (e.name === "return") { e.preventDefault(); props.palette.submitInput(); }
                else if (e.name === "escape") { e.preventDefault(); props.palette.pop(); }
              }}
            />
          </box>
        </Show>

        <Show when={palette().mode === "list"}>
          <Show when={palette().filterable}>
            <box flexDirection="row" gap={1} width="100%">
              <text flexBasis={1} fg={theme.textPrimary}>
                {">"}
              </text>
              <input
                flexGrow={1}
                focused
                value={palette().filterText}
                onInput={(value: string) => props.palette.filter(value)}
                onKeyDown={(e: KeyEvent) => {
                  if (e.name === "up") { e.preventDefault(); props.palette.moveUp(); }
                  else if (e.name === "down") { e.preventDefault(); props.palette.moveDown(); }
                  else if (e.name === "return") { e.preventDefault(); props.palette.selectCurrent(); }
                  else if (e.name === "escape") { e.preventDefault(); props.palette.pop(); }
                  else if (e.ctrl && e.name) {
                    const key = `ctrl+${e.name}`;
                    if (props.palette.handleKeyBinding(key)) { e.preventDefault(); }
                  }
                }}
              />
            </box>
          </Show>

          <Show when={palette().options.length === 0}>
            <text fg={theme.textMuted}>No results</text>
          </Show>

          <box flexDirection="row">
            <box flexGrow={1} flexDirection="column">
              <For each={visibleSlice().items}>
                {(entry) => {
                  const isFocused = () =>
                    entry.index === palette().selectedIndex;
                  const padded = () => entry.option.name.padEnd(maxNameLen());
                  return (
                    <box
                      flexDirection="row"
                      width="100%"
                      backgroundColor={
                        isFocused()
                          ? theme.pickerFocusedBg
                          : theme.bgTransparent
                      }
                    >
                      <text
                        fg={
                          isFocused()
                            ? theme.pickerFocusedText
                            : theme.pickerItemText
                        }
                        bg={
                          isFocused()
                            ? theme.pickerFocusedBg
                            : theme.bgTransparent
                        }
                      >
                        {padded()} {entry.option.description}
                      </text>
                    </box>
                  );
                }}
              </For>
            </box>
            <Show when={scrollbar()}>
              <box flexShrink={0} width={1} flexDirection="column">
                <For each={scrollbar()!}>
                  {(isThumb) => (
                    <text
                      fg={
                        isThumb
                          ? theme.pickerScrollThumb
                          : theme.pickerScrollTrack
                      }
                    >
                      {isThumb ? "█" : "│"}
                    </text>
                  )}
                </For>
              </box>
            </Show>
          </box>

          <Show when={palette().hint && palette().hint !== "__commands__"}>
            <text fg={theme.textMuted}>{palette().hint}</text>
          </Show>
        </Show>
      </box>
    </Show>
  );
}
