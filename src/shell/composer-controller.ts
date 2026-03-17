/**
 * Composer controller — owns all text-change orchestration, trigger
 * detection, reference pickers, and message submission.
 *
 * ComposerDock is a thin UI shell that delegates here.
 */

import type { AgentRuntime } from "../backend";
import type { FileIndex } from "../features/files";
import type { PagerController } from "../features/pager";
import type { ThreadIndex } from "../features/threads";
import { COMMANDS } from "../features/commands";
import { createPaletteManager, type PaletteManager } from "../state/palette-manager";

// ── Textarea handle ─────────────────────────────────────────────────

/**
 * The minimal slice of the textarea the controller needs.
 * Keeps all DOM/TUI internals inside ComposerDock.
 */
export type TextareaHandle = {
  plainText: string;
  cursorOffset: number;
  setText: (value: string) => void;
  insertText: (text: string) => void;
};

// ── Trigger helpers ─────────────────────────────────────────────────

function findDoubleAtPrefix(text: string, cursorOffset: number): string | null {
  const before = text.slice(0, cursorOffset);
  const match = before.match(/(?:^|\s)(@@[^\s]*)$/);
  return match ? match[1] : null;
}

function findAtPrefix(text: string, cursorOffset: number): string | null {
  const before = text.slice(0, cursorOffset);
  const match = before.match(/(?:^|\s)(@(?!@)[^\s]*)$/);
  return match ? match[1] : null;
}

// ── Controller ──────────────────────────────────────────────────────

export type ComposerControllerDeps = {
  runtime: AgentRuntime;
  fileIndex: FileIndex;
  threadIndex: ThreadIndex | null;
  pager: PagerController;
};

export function createComposerController(deps: ComposerControllerDeps) {
  const { runtime, fileIndex, threadIndex, pager } = deps;
  const palette: PaletteManager = createPaletteManager();

  let textareaRef: TextareaHandle | undefined;
  let filePickerActive = false;
  let threadPickerActive = false;
  let suppressAtTrigger = false;
  let suppressDoubleAtTrigger = false;
  let prevTextLength = 0;

  // ── Textarea binding ────────────────────────────────────────────

  function setTextarea(ref: TextareaHandle | undefined) {
    textareaRef = ref;
  }

  // ── Text replacement helpers ────────────────────────────────────

  function replacePrefix(prefix: string, replacement: string) {
    if (!textareaRef) return;
    const text = textareaRef.plainText;
    const cursorOffset = textareaRef.cursorOffset;
    const prefixStart = cursorOffset - prefix.length;

    if (prefixStart < 0) {
      textareaRef.insertText(replacement);
      return;
    }

    const before = text.slice(0, prefixStart);
    const after = text.slice(cursorOffset);
    const newText = before + replacement + after;
    const newCursorOffset = prefixStart + replacement.length;

    textareaRef.setText(newText);
    textareaRef.cursorOffset = newCursorOffset;
    prevTextLength = newText.length;
  }

  // ── Slash commands ──────────────────────────────────────────────

  function openSlashCommands() {
    palette.show({
      filterable: true,
      options: COMMANDS.map((cmd) => ({
        name: cmd.name,
        description: cmd.description,
        value: cmd,
        action: (ctx) => {
          textareaRef?.setText("");
          prevTextLength = 0;
          ctx.dismiss();
          cmd.execute({ runtime, palette, pager });
        },
      })),
    });
  }

  // ── File picker ─────────────────────────────────────────────────

  async function openFilePicker(atPrefix: string) {
    const query = atPrefix.slice(1);
    const suggestions = await fileIndex.suggest(query);
    if (suggestions.length === 0 || palette.visible) return;

    filePickerActive = true;
    palette.show({
      filterable: true,
      hint: "Select a file to reference",
      onDismiss: () => {
        filePickerActive = false;
        suppressAtTrigger = true;
      },
      onFilterChange: (text) => {
        // If the first keystroke in the file picker is '@', the user is
        // typing '@@' for a thread reference. Switch to the thread picker.
        if (text === "@") {
          palette.pop();
          // Append the second @ to the textarea so it reads '@@'
          if (textareaRef) {
            const currentText = textareaRef.plainText;
            const cursor = textareaRef.cursorOffset;
            // Insert @ at cursor position
            const before = currentText.slice(0, cursor);
            const after = currentText.slice(cursor);
            textareaRef.setText(before + "@" + after);
            textareaRef.cursorOffset = cursor + 1;
            prevTextLength = textareaRef.plainText.length;
          }
          suppressDoubleAtTrigger = false;
          const doublePrefix = findDoubleAtPrefix(
            textareaRef?.plainText ?? "",
            textareaRef?.cursorOffset ?? 0,
          );
          if (doublePrefix) {
            openThreadPicker(doublePrefix);
          }
          return false;
        }
      },
      options: suggestions.map((s) => ({
        name: s.name,
        description: s.description,
        value: s.value,
        action: (ctx) => {
          replacePrefix(atPrefix, `@${s.value} `);
          ctx.dismiss();
        },
      })),
    });
  }

  // ── Thread picker ───────────────────────────────────────────────

  async function openThreadPicker(doubleAtPrefix: string) {
    if (!threadIndex) return;
    const query = doubleAtPrefix.slice(2);
    const suggestions = await threadIndex.suggest(query);
    if (suggestions.length === 0 || palette.visible) return;

    threadPickerActive = true;
    palette.show({
      filterable: true,
      hint: "Select a thread to reference",
      onDismiss: () => {
        threadPickerActive = false;
        suppressDoubleAtTrigger = true;
      },
      options: suggestions.map((s) => ({
        name: s.name,
        description: s.description,
        value: s.value,
        action: (ctx) => {
          replacePrefix(doubleAtPrefix, `[[thread:${s.value}]] `);
          ctx.dismiss();
        },
      })),
    });
  }

  // ── Content change handler ──────────────────────────────────────

  function handleTextChange() {
    const text = textareaRef?.plainText ?? "";
    const grew = text.length > prevTextLength;
    prevTextLength = text.length;

    const trimmed = text.trimStart();

    // Slash command trigger (disabled in pager mode)
    if (trimmed === "/" && !palette.visible && !pager.active) {
      openSlashCommands();
      return;
    }

    // Clear suppression when user types a fresh '@'
    if (grew && !palette.visible) {
      const cursorOffset = textareaRef?.cursorOffset ?? text.length;
      if (cursorOffset > 0 && text[cursorOffset - 1] === "@") {
        if (cursorOffset > 1 && text[cursorOffset - 2] === "@") {
          suppressDoubleAtTrigger = false;
        } else {
          suppressAtTrigger = false;
        }
      }
    }

    // Trigger detection: only on text growth, not suppressed, check @@ before @
    if (!palette.visible && grew) {
      const cursorOffset = textareaRef?.cursorOffset ?? text.length;

      if (!suppressDoubleAtTrigger) {
        const doublePrefix = findDoubleAtPrefix(text, cursorOffset);
        if (doublePrefix) {
          openThreadPicker(doublePrefix);
          return;
        }
      }

      if (!suppressAtTrigger) {
        const prefix = findAtPrefix(text, cursorOffset);
        if (prefix) {
          openFilePicker(prefix);
        }
      }
    }
  }

  // ── Submit handler ──────────────────────────────────────────────

  async function handleSubmit() {
    if (palette.visible && !palette.isFilterable) {
      palette.selectCurrent();
      return;
    }
    if (palette.visible) return;

    const text = textareaRef?.plainText ?? "";

    // Pager mode: notes auto-save on navigation, ignore Enter submit
    if (pager.active) {
      return;
    }

    if (!text.trim()) return;

    textareaRef?.setText("");
    prevTextLength = 0;
    try {
      await runtime.submitUserMessage(text);
    } catch (error) {
      console.error(error);
      textareaRef?.setText(text);
      prevTextLength = text.length;
    }
  }

  function insertText(text: string) {
    if (!textareaRef) return;
    textareaRef.insertText(text);
    prevTextLength = textareaRef.plainText.length;
  }

  function getTextareaText(): string {
    return textareaRef?.plainText ?? "";
  }

  function setTextareaText(text: string) {
    textareaRef?.setText(text);
    prevTextLength = text.length;
  }

  function recallLastUserMessage() {
    const messages = runtime.getMessages();
    for (let i = messages.length - 1; i >= 0; i--) {
      const msg = messages[i];
      if (msg.role !== "user") continue;
      const content = (msg as { content?: unknown }).content;
      let text = "";
      if (typeof content === "string") {
        text = content;
      } else if (Array.isArray(content)) {
        text = content
          .filter((b: any) => b?.type === "text" && typeof b.text === "string")
          .map((b: any) => b.text)
          .join("\n");
      }
      if (text.trim()) {
        setTextareaText(text);
        if (textareaRef) {
          textareaRef.cursorOffset = text.length;
        }
        return;
      }
    }
  }

  return {
    palette,
    setTextarea,
    handleTextChange,
    handleSubmit,
    insertText,
    getTextareaText,
    setTextareaText,
    recallLastUserMessage,
  };
}

export type ComposerController = ReturnType<typeof createComposerController>;
