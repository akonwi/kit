import type { KeyEvent } from "@opentui/core";
import { createSignal } from "solid-js";
import { useKeyboard } from "@opentui/solid";
import type { ComposerController, TextareaHandle } from "./composer-controller";
import { InlinePicker } from "./InlinePicker";
import { theme } from "./theme";

export type ComposerDockProps = {
  cwd: string;
  sessionName: string | undefined;
  controller: ComposerController;
  onHeightChange?: (height: number) => void;
};

const STATUS_BAR_HEIGHT = 1;

export function ComposerDock(props: ComposerDockProps) {
  let dockRef: { width: number; height: number } | undefined;
  const [dockHeight, setDockHeight] = createSignal(3);
  const palette = props.controller.palette;

  // Keyboard handler for non-filterable palette navigation.
  // Filterable/input pickers are handled by InlinePicker's own <input>.
  useKeyboard((e: KeyEvent) => {
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

  function handleSizeChange() {
    if (!dockRef) return;
    setDockHeight(dockRef.height);
    props.onHeightChange?.(dockRef.height);
  }

  return (
    <box flexShrink={0} flexDirection="column">
      <box
        flexShrink={0}
        ref={(value) => { dockRef = value as typeof dockRef; }}
        onSizeChange={handleSizeChange}
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
            placeholder="Ask pi-kit to do something..."
            placeholderColor={theme.textPlaceholder}
            backgroundColor={theme.bgSurface}
            focusedBackgroundColor={theme.bgSurface}
            textColor={theme.textPrimary}
            focusedTextColor={theme.textPrimary}
            cursorColor={theme.cursor}
            showCursor={!palette.visible}
            wrapMode="word"
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
        </text>
      </box>
      <InlinePicker
        palette={palette}
        bottomOffset={dockHeight() + STATUS_BAR_HEIGHT + 2}
      />
    </box>
  );
}
