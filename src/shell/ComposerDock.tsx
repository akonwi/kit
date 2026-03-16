import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import type { AgentRuntime } from "../backend";
import type { FileIndex } from "../features/files";
import type { PaletteManager } from "../state/palette-manager";
import { theme } from "./theme";

export type ComposerDockProps = {
  cwd: string;
  sessionName: string | undefined;
  palette: PaletteManager;
  runtime: AgentRuntime;
  fileIndex: FileIndex;
  onHeightChange?: (height: number) => void;
};

type TextareaRef = {
  plainText: string;
  cursorOffset: number;
  setText: (value: string) => void;
  insertText: (text: string) => void;
};

/**
 * Find the `@query` token immediately before the cursor, if any.
 * Returns the full `@...` prefix string, or null if no trigger is active.
 */
function findAtPrefix(text: string, cursorOffset: number): string | null {
  const before = text.slice(0, cursorOffset);
  // Match @<non-whitespace> at the end, preceded by start-of-string or whitespace
  const match = before.match(/(?:^|\s)(@[^\s]*)$/);
  if (!match) return null;
  return match[1]; // the "@..." part
}

export function ComposerDock(props: ComposerDockProps) {
  let dockRef: { width: number; height: number } | undefined;
  let textareaRef: TextareaRef | undefined;

  // Track whether the current palette was opened by the @ trigger
  // so we don't interfere with slash commands or other palettes.
  let filePickerActive = false;

  // Suppress @ trigger after user dismisses the file picker (escape / backspace).
  // Cleared when the user types a fresh '@' character.
  let suppressAtTrigger = false;

  // Track previous text length to distinguish typing (growth) from deletion.
  let prevTextLength = 0;

  function handleTextChange() {
    const text = textareaRef?.plainText ?? "";
    const grew = text.length > prevTextLength;
    prevTextLength = text.length;

    const trimmed = text.trimStart();

    // Slash command trigger
    if (trimmed === "/" && !props.palette.visible) {
      props.palette.showCommands(props.runtime, {
        onSelect: () => {
          textareaRef?.setText("");
          prevTextLength = 0;
        },
      });
      return;
    }

    // Clear suppression when the user types a fresh '@'
    if (grew && !props.palette.visible) {
      const cursorOffset = textareaRef?.cursorOffset ?? text.length;
      if (cursorOffset > 0 && text[cursorOffset - 1] === "@") {
        suppressAtTrigger = false;
      }
    }

    // File reference trigger: only on text growth, not suppressed
    if (!props.palette.visible && grew && !suppressAtTrigger) {
      const cursorOffset = textareaRef?.cursorOffset ?? text.length;
      const prefix = findAtPrefix(text, cursorOffset);
      if (prefix) {
        openFilePicker(prefix);
      }
    }
  }

  async function openFilePicker(atPrefix: string) {
    const query = atPrefix.slice(1); // strip leading @
    const suggestions = await props.fileIndex.suggest(query);
    if (suggestions.length === 0) return;
    // If palette was opened by something else while we were scanning, bail
    if (props.palette.visible) return;

    filePickerActive = true;
    props.palette.show({
      filterable: true,
      hint: "Select a file to reference",
      onDismiss: () => {
        filePickerActive = false;
        suppressAtTrigger = true;
      },
      options: suggestions.map((s) => ({
        name: s.name,
        description: s.description,
        value: s.value,
        action: (ctx) => {
          insertFileReference(atPrefix, s.value);
          ctx.dismiss();
        },
      })),
    });
  }

  function insertFileReference(atPrefix: string, filePath: string) {
    if (!textareaRef) return;
    const text = textareaRef.plainText;
    const cursorOffset = textareaRef.cursorOffset;

    // Find where the @prefix starts in the text before cursor
    const prefixStart = cursorOffset - atPrefix.length;
    if (prefixStart < 0) {
      // Fallback: just append
      textareaRef.insertText(`@${filePath} `);
      return;
    }

    // Replace the @prefix with @filePath
    const before = text.slice(0, prefixStart);
    const after = text.slice(cursorOffset);
    const replacement = `@${filePath} `;
    const newText = before + replacement + after;
    const newCursorOffset = prefixStart + replacement.length;

    textareaRef.setText(newText);
    textareaRef.cursorOffset = newCursorOffset;
    prevTextLength = newText.length;
  }

  useKeyboard((e: KeyEvent) => {
    const pm = props.palette;
    if (!pm.visible) return;

    // Filterable/input pickers are handled by InlinePicker's <input>
    if (pm.isFilterable || pm.isInputMode) return;

    // Non-filterable pickers (e.g. /thinking)
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
      pm.pop();
      return;
    }
    if (e.name === "return") {
      e.preventDefault();
      pm.selectCurrent();
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

    // Block all other keystrokes
    e.preventDefault();
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
    <box
      flexShrink={0}
      ref={(value) => { dockRef = value as typeof dockRef; }}
      onSizeChange={() => {
        if (dockRef) props.onHeightChange?.(dockRef.height);
      }}
    >
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
          minHeight={1}
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
