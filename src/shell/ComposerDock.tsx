import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import type { AgentRuntime } from "../backend";
import type { PaletteManager } from "../state/palette-manager";
import { theme } from "./theme";

export type ComposerDockProps = {
  cwd: string;
  sessionName: string | undefined;
  palette: PaletteManager;
  runtime: AgentRuntime;
};

export function ComposerDock(props: ComposerDockProps) {
  let textareaRef:
    | {
        plainText: string;
        setText: (value: string) => void;
      }
    | undefined;

  let filterText = "";
  let inputText = "";
  let lastInputMode = false;

  function handleTextChange() {
    const text = textareaRef?.plainText ?? "";
    const trimmed = text.trimStart();

    if (trimmed === "/" && !props.palette.visible) {
      props.palette.showCommands(props.runtime, {
        onSelect: () => textareaRef?.setText(""),
      });
    }
  }

  useKeyboard((e: KeyEvent) => {
    const pm = props.palette;

    // Seed inputText when entering input mode
    if (pm.isInputMode && !lastInputMode) {
      inputText = pm.inputValue;
    }
    lastInputMode = pm.isInputMode;

    // Input mode (rename prompt etc.)
    if (pm.isInputMode) {
      e.preventDefault();
      if (e.name === "return") {
        pm.submitInput();
        inputText = "";
      } else if (e.name === "escape") {
        pm.pop();
        inputText = "";
      } else if (e.name === "backspace") {
        inputText = inputText.slice(0, -1);
        pm.setInputValue(inputText);
      } else if (e.sequence && e.sequence.length === 1 && !e.ctrl && !e.meta) {
        inputText += e.sequence;
        pm.setInputValue(inputText);
      }
      return;
    }

    if (!pm.visible) return;

    if (e.name === "up") {
      e.preventDefault();
      pm.moveUp();
      return;
    }
    if (e.name === "down") {
      e.preventDefault();
      pm.moveDown();
      return;
    }
    if (e.name === "escape") {
      e.preventDefault();
      filterText = "";
      pm.pop();
      return;
    }

    // Ctrl keybindings
    if (e.ctrl && e.name) {
      const key = `ctrl+${e.name}`;
      if (pm.handleKeyBinding(key)) {
        e.preventDefault();
        return;
      }
    }

    // Enter selects current option in any visible picker
    if (e.name === "return") {
      e.preventDefault();
      filterText = "";
      pm.selectCurrent();
      return;
    }

    // Non-filterable pickers block all remaining keystrokes
    if (!pm.isFilterable) {
      e.preventDefault();
      return;
    }

    // Filterable text input
    if (pm.isFilterable) {
      e.preventDefault();
      if (e.name === "backspace") {
        filterText = filterText.slice(0, -1);
        pm.filter(filterText);
      } else if (e.sequence && e.sequence.length === 1 && !e.ctrl && !e.meta) {
        filterText += e.sequence;
        pm.filter(filterText);
      }
      return;
    }
  });

  async function handleSubmit() {
    const pm = props.palette;
    if (pm.visible && !pm.isFilterable) {
      pm.selectCurrent();
      return;
    }
    if (pm.visible) return;

    const text = textareaRef?.plainText ?? "";
    if (!text.trim()) return;

    textareaRef?.setText("");
    try {
      await props.runtime.submitUserMessage(text);
    } catch (error) {
      console.error(error);
      textareaRef?.setText(text);
    }
  }

  return (
    <box flexShrink={0}>
      <box
        width="100%"
        border
        borderColor={theme.borderFocused}
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
          height={6}
          placeholder="Ask pi-kit to do something..."
          placeholderColor={theme.textPlaceholder}
          backgroundColor={theme.bgSurface}
          focusedBackgroundColor={theme.bgSurface}
          textColor={theme.textPrimary}
          focusedTextColor={theme.textPrimary}
          cursorColor={theme.cursor}
          showCursor={!props.palette.visible}
          wrapMode="word"
          keyBindings={[
            { name: "return", action: "submit" },
            { name: "linefeed", action: "submit" },
            { name: "return", shift: true, action: "newline" },
          ]}
          onContentChange={() => {
            handleTextChange();
          }}
          onSubmit={handleSubmit}
          focused={!props.palette.visible}
        />
      </box>
      <text position="absolute" bottom={0} left={2} fg={theme.textMuted}>
        {props.sessionName || "Unnamed"}
      </text>
      <text position="absolute" bottom={0} right={2} fg={theme.textMuted}>
        {props.cwd}
      </text>
    </box>
  );
}
