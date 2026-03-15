import type { KeyEvent, SelectOption } from "@opentui/core";
import { Show } from "solid-js";
import type { PickerOption, PickerState } from "../state/app-state";

export type InlinePickerProps = {
  picker: PickerState;
  onSelect: (option: PickerOption) => void;
  onDismiss: () => void;
};

export function InlinePicker(props: InlinePickerProps) {
  const visible = () => props.picker.visible && props.picker.mode === "inline";
  const optionCount = () => props.picker.options.length;
  const selectHeight = () => Math.min(optionCount(), 8);

  return (
    <Show when={visible()}>
      <box
        flexShrink={0}
        border
        borderColor="white"
        paddingLeft={1}
        paddingRight={1}
        onKeyDown={(e: KeyEvent) => {
          if (e.name === "escape") {
            props.onDismiss();
          }
        }}
      >
        <select
          focused
          width="100%"
          height={selectHeight()}
          options={
            props.picker.options.map((o): SelectOption => ({
              name: o.name,
              description: o.description,
            }))
          }
          selectedIndex={props.picker.selectedIndex}
          backgroundColor="transparent"
          textColor="#8f8f8f"
          focusedBackgroundColor="transparent"
          focusedTextColor="#8f8f8f"
          selectedBackgroundColor="transparent"
          selectedTextColor="#f2f2f2"
          descriptionColor="#555555"
          selectedDescriptionColor="#8f8f8f"
          showDescription
          wrapSelection
          showScrollIndicator
          onSelect={(_index: number, option: SelectOption | null) => {
            if (!option) return;
            const picked = props.picker.options.find((o) => o.name === option.name);
            if (picked) {
              props.onSelect(picked);
            }
          }}
        />
      </box>
    </Show>
  );
}
