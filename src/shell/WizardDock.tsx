import type { KeyEvent, PasteEvent } from "@opentui/core";
import { For, Show } from "solid-js";
import { useKeyboard } from "@opentui/solid";
import type { WizardController } from "../features/wizard";
import { theme } from "./theme";

export type WizardDockProps = {
  wizard: WizardController;
};

export function WizardDock(props: WizardDockProps) {
  const w = props.wizard;
  let textareaRef: { plainText: string; setText: (v: string) => void } | undefined;

  useKeyboard((e: KeyEvent) => {
    if (!w.active) return;

    // Escape — cancel wizard or escape from otherText
    if (e.name === "escape") {
      e.preventDefault();
      if (w.mode === "otherText") {
        w.escapeTextMode();
      } else {
        w.cancel();
      }
      return;
    }

    // Shift+Tab — go to previous question
    if (e.shift && e.name === "tab") {
      e.preventDefault();
      w.movePrev();
      // Load previous answer into textarea if in text mode
      if (w.mode === "text" || w.mode === "otherText") {
        const prev = w.answers[w.currentQuestion?.id ?? ""];
        textareaRef?.setText(typeof prev === "string" ? prev : "");
      }
      return;
    }

    // Select mode
    if (w.mode === "select") {
      if (e.name === "up") { e.preventDefault(); w.moveSelectUp(); return; }
      if (e.name === "down") { e.preventDefault(); w.moveSelectDown(); return; }
      if (e.name === "return") {
        e.preventDefault();
        w.selectOption();
        // selectOption may switch mode to otherText — load existing answer
        const newMode = w.mode as string;
        if (newMode === "otherText" || newMode === "text") {
          const existing = w.answers[w.currentQuestion?.id ?? ""];
          textareaRef?.setText(typeof existing === "string" ? existing : "");
        }
        return;
      }
      // Block other keys in select mode
      e.preventDefault();
      return;
    }

    // Text / otherText mode — Enter submits
    if (w.mode === "text" || w.mode === "otherText") {
      if (e.name === "return" && !e.shift) {
        e.preventDefault();
        const text = textareaRef?.plainText ?? "";
        w.submitText(text);
        textareaRef?.setText("");
        // Load next question's answer if in text mode
        if (w.active && (w.mode === "text" || w.mode === "otherText")) {
          const next = w.answers[w.currentQuestion?.id ?? ""];
          textareaRef?.setText(typeof next === "string" ? next : "");
        }
        return;
      }
      // Let other keys pass through to textarea
    }
  });

  function handlePaste(event: PasteEvent) {
    if (w.mode === "text" || w.mode === "otherText") {
      const text = event.text.replace(/\r\n/g, "\n").replace(/\r/g, "\n").trim();
      if (text && textareaRef) {
        textareaRef.setText(textareaRef.plainText + text);
      }
    }
  }

  const placeholder = () => {
    if (w.mode === "otherText") return "Type your custom answer...";
    return w.currentQuestion?.placeholder || "Type your answer...";
  };

  return (
    <box flexShrink={0}>
      <box
        width="100%"
        border
        borderColor={theme.borderAccent}
        paddingLeft={1}
        paddingRight={1}
        paddingBottom={1}
        flexDirection="column"
        gap={0}
      >
        <Show when={w.mode === "select"}>
          <For each={w.currentQuestion ? w.getSelectOptions(w.currentQuestion) : []}>
            {(option, idx) => {
              const isFocused = () => idx() === w.selectIndex;
              return (
                <box
                  flexDirection="row"
                  width="100%"
                  backgroundColor={isFocused() ? theme.pickerFocusedBg : theme.bgTransparent}
                >
                  <text
                    fg={isFocused() ? theme.pickerFocusedText : theme.textPrimary}
                    bg={isFocused() ? theme.pickerFocusedBg : theme.bgTransparent}
                  >
                    {isFocused() ? "› " : "  "}{option}
                  </text>
                </box>
              );
            }}
          </For>
        </Show>

        <Show when={w.mode === "text" || w.mode === "otherText"}>
          <Show when={w.mode === "otherText"}>
            <text fg={theme.borderAccent}>Specify Other:</text>
          </Show>
          {/* @ts-ignore onPaste supported but not typed */}
          <textarea
            ref={(value) => { textareaRef = value as typeof textareaRef; }}
            minHeight={1}
            placeholder={placeholder()}
            placeholderColor={theme.textPlaceholder}
            backgroundColor={theme.bgSurface}
            focusedBackgroundColor={theme.bgSurface}
            textColor={theme.textPrimary}
            focusedTextColor={theme.textPrimary}
            cursorColor={theme.cursor}
            showCursor
            wrapMode="word"
            keyBindings={[
              { name: "return", shift: true, action: "newline" },
            ]}
            focused={w.mode === "text" || w.mode === "otherText"}
            onPaste={handlePaste}
          />
        </Show>
      </box>
    </box>
  );
}
