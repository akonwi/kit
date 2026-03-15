import { createMemo, For, Show } from "solid-js";
import type { PaletteSnapshot } from "../state/palette";
import { theme } from "./theme";

const MAX_VISIBLE = 10;

export type InlinePickerProps = {
  palette: PaletteSnapshot;
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
  const maxNameLen = () =>
    props.palette.options.reduce((max, o) => Math.max(max, o.name.length), 0);

  const visibleSlice = createMemo(() => {
    const options = props.palette.options;
    const count = options.length;
    const selected = props.palette.selectedIndex;

    if (count <= MAX_VISIBLE) {
      return { items: options.map((o, i) => ({ option: o, index: i })), offset: 0 };
    }

    let offset = selected - Math.floor(MAX_VISIBLE / 2);
    offset = Math.max(0, Math.min(offset, count - MAX_VISIBLE));

    const items = options
      .slice(offset, offset + MAX_VISIBLE)
      .map((o, i) => ({ option: o, index: offset + i }));

    return { items, offset };
  });

  const scrollbar = createMemo(() =>
    computeScrollbar(props.palette.options.length, MAX_VISIBLE, visibleSlice().offset),
  );

  return (
    <Show when={props.palette.visible}>
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
        <Show when={props.palette.mode === "input"}>
          <text fg={theme.textMuted}>{props.palette.label}</text>
          <text fg={theme.textPrimary}>{"> "}{props.palette.inputValue}</text>
        </Show>

        <Show when={props.palette.mode === "list"}>
          <Show when={props.palette.filterable}>
            <text fg={theme.textPrimary}>{"> "}{props.palette.filterText}</text>
            <text> </text>
          </Show>

          <Show when={props.palette.options.length === 0}>
            <text fg={theme.textMuted}>No results</text>
          </Show>

          <box flexDirection="row">
            <box flexGrow={1} flexDirection="column">
              <For each={visibleSlice().items}>
                {(entry) => {
                  const isFocused = () => entry.index === props.palette.selectedIndex;
                  const padded = () => entry.option.name.padEnd(maxNameLen());
                  return (
                    <box
                      flexDirection="row"
                      width="100%"
                      backgroundColor={isFocused() ? theme.pickerFocusedBg : theme.bgTransparent}
                    >
                      <text
                        fg={isFocused() ? theme.pickerFocusedText : theme.pickerItemText}
                        bg={isFocused() ? theme.pickerFocusedBg : theme.bgTransparent}
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
                    <text fg={isThumb ? theme.pickerScrollThumb : theme.pickerScrollTrack}>
                      {isThumb ? "█" : "│"}
                    </text>
                  )}
                </For>
              </box>
            </Show>
          </box>

          <Show when={props.palette.hint && props.palette.hint !== "__commands__"}>
            <text fg={theme.textMuted}>{props.palette.hint}</text>
          </Show>
        </Show>
      </box>
    </Show>
  );
}
