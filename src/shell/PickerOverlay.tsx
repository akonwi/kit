import type { KeyEvent, SelectOption } from "@opentui/core";
import { Show } from "solid-js";
import type { PickerOption, PickerState } from "../state/app-state";

export type PickerOverlayProps = {
  picker: PickerState;
  onSelect: (option: PickerOption) => void;
  onDismiss: () => void;
};

export function PickerOverlay(props: PickerOverlayProps) {
  return (
    <Show when={props.picker.visible}>
      <box
        position="absolute"
        top={0}
        left={0}
        width="100%"
        height="100%"
        justifyContent="center"
        alignItems="center"
        onKeyDown={(e: KeyEvent) => {
          if (e.name === "escape") {
            props.onDismiss();
          }
        }}
      >
        <box
          flexDirection="column"
          border
          borderColor="#6cb6ff"
          width="60%"
          maxHeight="80%"
          backgroundColor="#1a1a2e"
          padding={1}
          gap={1}
        >
          <text fg="#6cb6ff">{props.picker.title}</text>
          <select
            focused
            flexGrow={1}
            options={
              props.picker.options.map((o): SelectOption => ({
                name: o.name,
                description: o.description,
              }))
            }
            selectedIndex={props.picker.selectedIndex}
            textColor="#b8b8b8"
            focusedTextColor="#ffffff"
            focusedBackgroundColor="#2f6e9b"
            descriptionColor="#666666"
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
          <text fg="#666666">↑↓ navigate · Enter select · Esc cancel</text>
        </box>
      </box>
    </Show>
  );
}
