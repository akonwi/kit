import type { KeyEvent } from "@opentui/core";
import { createEffect } from "solid-js";
import { useKeyboard } from "@opentui/solid";
import type { ComposerState } from "../state/app-state";

export type ComposerDockProps = {
  composer: ComposerState;
  cwd: string;
  sessionName: string | undefined;
  pickerVisible: boolean;
  pickerFilterable: boolean;
  onContentChange: (value: string) => void;
  onSubmit: () => void;
  onPickerUp: () => void;
  onPickerDown: () => void;
  onPickerSelect: () => void;
  onPickerDismiss: () => void;
  onPickerFilter: (query: string) => void;
};

export function ComposerDock(props: ComposerDockProps) {
  let textareaRef: {
    plainText: string;
    setText: (value: string) => void;
  } | undefined;

  let filterText = "";

  createEffect(() => {
    const nextText = props.composer.text;
    if (!textareaRef) return;
    if (textareaRef.plainText !== nextText) {
      textareaRef.setText(nextText);
    }
  });

  // Global key handler — fires before the textarea processes the key
  useKeyboard((e: KeyEvent) => {
    if (!props.pickerVisible) return;

    if (e.name === "up") {
      e.preventDefault();
      props.onPickerUp();
      return;
    }
    if (e.name === "down") {
      e.preventDefault();
      props.onPickerDown();
      return;
    }
    if (e.name === "escape") {
      e.preventDefault();
      filterText = "";
      props.onPickerDismiss();
      return;
    }
    // For filterable pickers, intercept typing
    if (props.pickerFilterable) {
      e.preventDefault();
      if (e.name === "backspace") {
        filterText = filterText.slice(0, -1);
        props.onPickerFilter(filterText);
      } else if (e.sequence && e.sequence.length === 1 && !e.ctrl && !e.meta) {
        filterText += e.sequence;
        props.onPickerFilter(filterText);
      }
    }
  });

  return (
    <box flexShrink={0}>
      <box
        width="100%"
        border
        borderColor="white"
        paddingLeft={1}
        paddingRight={1}
        paddingBottom={1}
        flexDirection="column"
        gap={0}
      >
        <textarea
          ref={(value) => {
            textareaRef = value as typeof textareaRef;
          }}
          height={props.composer.height}
          initialValue={props.composer.text}
          placeholder={props.composer.placeholder}
          placeholderColor="#666666"
          backgroundColor="#1b1b1b"
          focusedBackgroundColor="#1b1b1b"
          textColor="#f2f2f2"
          focusedTextColor="#f2f2f2"
          cursorColor="#ffffff"
          showCursor={!props.pickerFilterable}
          wrapMode="word"
          keyBindings={[
            { name: "return", action: "submit" },
            { name: "linefeed", action: "submit" },
            { name: "return", shift: true, action: "newline" },
          ]}
          onContentChange={() => {
            props.onContentChange(textareaRef?.plainText ?? "");
          }}
          onSubmit={() => {
            if (props.pickerVisible) {
              filterText = "";
              props.onPickerSelect();
            } else {
              props.onSubmit();
            }
          }}
          focused
        />
      </box>
      <text position="absolute" bottom={0} left={2} fg="#8f8f8f">
        {props.sessionName || "Unnamed"}
      </text>
      <text position="absolute" bottom={0} right={2} fg="#8f8f8f">
        {props.cwd}
      </text>
    </box>
  );
}
