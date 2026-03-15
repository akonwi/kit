import { createMemo, For, Show } from "solid-js";
import type { PickerState } from "../state/app-state";
import { theme } from "./theme";

const MAX_VISIBLE = 10;

export type InlinePickerProps = {
  picker: PickerState;
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
  const visible = () => props.picker.visible;
  const maxNameLen = () =>
    props.picker.options.reduce((max, o) => Math.max(max, o.name.length), 0);

  const visibleSlice = createMemo(() => {
    const options = props.picker.options;
    const count = options.length;
    const selected = props.picker.selectedIndex;

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
      props.picker.options.length,
      MAX_VISIBLE,
      visibleSlice().offset,
    ),
  );

  return (
    <Show when={visible()}>
      <box
        position="absolute"
        bottom={12}
        left={0}
        width="100%"
        border
        borderColor={theme.pickerBorder}
        backgroundColor={theme.pickerBg}
        paddingX={1}
        flexDirection="row"
      >
        <box flexGrow={1} flexDirection="column">
          <Show when={props.picker.filterable}>
            <text fg={theme.textPrimary}>
              {"> "}
              {props.picker.filterText}
            </text>
            <text> </text>
          </Show>
          <Show when={props.picker.options.length === 0}>
            <text fg={theme.textMuted}>No results</text>
          </Show>
          <For each={visibleSlice().items}>
            {(entry) => {
              const isFocused = () =>
                entry.index === props.picker.selectedIndex;
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
    </Show>
  );
}
