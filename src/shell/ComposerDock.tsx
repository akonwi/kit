import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import type { PagerController } from "../features/pager";
import type { ComposerController, TextareaHandle } from "./composer-controller";
import { theme } from "./theme";

export type ComposerDockProps = {
  cwd: string;
  sessionName: string | undefined;
  gitBranch: string | null;
  gitDirty: boolean;
  controller: ComposerController;
  pager: PagerController;
  onHeightChange?: (height: number) => void;
};

export function ComposerDock(props: ComposerDockProps) {
  let dockRef: { width: number; height: number } | undefined;
  const palette = props.controller.palette;
  const pager = props.pager;

  useKeyboard((e: KeyEvent) => {
    // Ctrl+C — clear composer if it has content, otherwise quit
    if (e.ctrl && e.name === "c") {
      e.preventDefault();
      if (palette.visible) {
        palette.clear();
        return;
      }
      const text = props.controller.getTextareaText();
      if (text.trim()) {
        props.controller.setTextareaText("");
        return;
      }
      // Empty composer — quit the app
      props.controller.quit();
      return;
    }

    // Escape — abort agent when composer is empty and agent is working
    if (
      e.name === "escape" &&
      !pager.active &&
      !palette.visible &&
      !props.controller.getTextareaText().trim() &&
      props.controller.isStreaming()
    ) {
      e.preventDefault();
      props.controller.abort();
      return;
    }

    // Up arrow in empty composer — recall last user message
    if (
      e.name === "up" &&
      !pager.active &&
      !palette.visible &&
      !props.controller.getTextareaText().trim()
    ) {
      e.preventDefault();
      props.controller.recallLastUserMessage();
      return;
    }

    // Alt+Enter — queue composer text as follow-up (processed after agent finishes)
    if (
      e.option &&
      (e.name === "return" || e.name === "enter") &&
      !pager.active &&
      !palette.visible &&
      props.controller.getTextareaText().trim()
    ) {
      e.preventDefault();
      void props.controller.handleFollowUp();
      return;
    }

    // Alt+Enter — queue composer text as follow-up (processed after agent finishes)
    if (
      e.option &&
      (e.name === "return" || e.name === "enter") &&
      !pager.active &&
      !palette.visible &&
      props.controller.getTextareaText().trim()
    ) {
      e.preventDefault();
      void props.controller.handleFollowUp();
      return;
    }

    // Alt+Up — restore queued steering/follow-up messages to composer
    if (
      e.option &&
      e.name === "up" &&
      !pager.active &&
      !palette.visible
    ) {
      e.preventDefault();
      props.controller.restorePendingMessages();
      return;
    }

    // Pager navigation
    if (pager.active && !palette.visible) {
      if (e.name === "escape") {
        e.preventDefault();
        // Save current note before closing
        const text = props.controller.getTextareaText();
        pager.setNote(pager.currentIndex, text);
        pager.close();
        props.controller.setTextareaText("");
        return;
      }
      // Ctrl+Shift+Right — next section (save current note first)
      if (e.ctrl && e.shift && e.name === "right") {
        e.preventDefault();
        // Save current note before navigating
        const text = props.controller.getTextareaText();
        pager.setNote(pager.currentIndex, text);
        pager.nextSection();
        // Load note for new section
        props.controller.setTextareaText(pager.notes.get(pager.currentIndex) ?? "");
        return;
      }
      // Ctrl+Shift+Left — prev section
      if (e.ctrl && e.shift && e.name === "left") {
        e.preventDefault();
        const text = props.controller.getTextareaText();
        pager.setNote(pager.currentIndex, text);
        pager.prevSection();
        props.controller.setTextareaText(pager.notes.get(pager.currentIndex) ?? "");
        return;
      }
      // Ctrl+Up/Down — scroll current page
      if (e.ctrl && e.name === "up") {
        e.preventDefault();
        pager.scrollUp();
        return;
      }
      if (e.ctrl && e.name === "down") {
        e.preventDefault();
        pager.scrollDown();
        return;
      }
      // Ctrl+Enter — submit all notes as feedback
      if (e.ctrl && e.name === "return") {
        e.preventDefault();
        const text = props.controller.getTextareaText();
        pager.setNote(pager.currentIndex, text);
        pager.submitFeedback();
        props.controller.setTextareaText("");
        return;
      }
    }

    // Non-filterable palette navigation
    if (!palette.visible) return;
    if (palette.isFilterable || palette.isInputMode) return;

    if (e.name === "up") { e.preventDefault(); palette.moveUp(); return; }
    if (e.name === "down") { e.preventDefault(); palette.moveDown(); return; }
    if (e.name === "escape") { e.preventDefault(); palette.pop(); return; }
    if (e.name === "return") { e.preventDefault(); palette.selectCurrent(); return; }

    if (e.ctrl && e.name) {
      const key = `ctrl+${e.name}`;
      if (palette.handleKeyBinding(key)) { e.preventDefault(); return; }
    }

    e.preventDefault();
  });

  const placeholder = () =>
    pager.active
      ? "Add a note for this section... (Ctrl+Enter to submit all)"
      : "Ask pi-kit to do something...";

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
            props.controller.setTextarea(value as TextareaHandle | undefined);
          }}
          minHeight={1}
          placeholder={placeholder()}
          placeholderColor={theme.textPlaceholder}
          backgroundColor={theme.bg}
          focusedBackgroundColor={theme.bg}
          textColor={theme.textPrimary}
          focusedTextColor={theme.textPrimary}
          cursorColor={theme.cursor}
          showCursor={!palette.visible}
          wrapMode="word"
          maxHeight={10}
          overflow="scroll"
          keyBindings={[
            { name: "return", action: "submit" },
            { name: "linefeed", action: "submit" },
            { name: "return", shift: true, action: "newline" },
          ]}
          onContentChange={() => props.controller.handleTextChange()}
          onSubmit={() => props.controller.handleSubmit()}
          focused={!palette.visible}
        />
      </box>
      <text position="absolute" bottom={0} left={2} fg={theme.textMuted}>
        {props.sessionName || "Unnamed"}
      </text>
      <text position="absolute" bottom={0} right={2} fg={theme.textMuted}>
        {props.cwd}
        {props.gitBranch && ` (${props.gitBranch}${props.gitDirty ? " ●" : " ○"})`}
      </text>
    </box>
  );
}
