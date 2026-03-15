import { For, Show } from "solid-js";
import type { PickerState } from "../state/app-state";

export type InlinePickerProps = {
  picker: PickerState;
};

export function InlinePicker(props: InlinePickerProps) {
  const visible = () => props.picker.visible && props.picker.mode === "inline";
  const maxNameLen = () =>
    props.picker.options.reduce((max, o) => Math.max(max, o.name.length), 0);

  return (
    <Show when={visible()}>
      <box
        flexShrink={0}
        border
        borderColor="white"
        paddingLeft={1}
        paddingRight={1}
        flexDirection="column"
      >
        <For each={props.picker.options}>
          {(option, i) => {
            const isFocused = () => i() === props.picker.selectedIndex;
            const padded = () => option.name.padEnd(maxNameLen());
            return (
              <box
                flexDirection="row"
                width="100%"
                backgroundColor={isFocused() ? "white" : "transparent"}
              >
                <text
                  fg={isFocused() ? "#000000" : "#8f8f8f"}
                  bg={isFocused() ? "white" : "transparent"}
                >
                  {padded()}  {option.description}
                </text>
              </box>
            );
          }}
        </For>
      </box>
    </Show>
  );
}
