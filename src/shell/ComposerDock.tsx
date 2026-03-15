import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import type { AgentRuntime } from "../backend";
import type { PaletteManager } from "../state/palette-manager";
import { matchCommands } from "../features/command-registry";
import { executeCommand } from "../features/commands";
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
  let commandPaletteActive = false;

  function handleTextChange() {
    const text = textareaRef?.plainText ?? "";
    const trimmed = text.trimStart();

    if (trimmed.startsWith("/") && !trimmed.includes(" ")) {
      const matches = matchCommands(trimmed);
      if (matches.length > 0) {
        const options = matches.map((c) => ({
          name: c.name,
          description: c.description,
          value: c,
          action: (ctx: { dismiss: () => void }) => {
            commandPaletteActive = false;
            ctx.dismiss();
            handleSlashCommand(c.name);
          },
        }));
        if (commandPaletteActive) {
          props.palette.updateTopOptions(options);
        } else {
          props.palette.show({ options });
          commandPaletteActive = true;
        }
        return;
      }
    }

    if (commandPaletteActive) {
      commandPaletteActive = false;
      props.palette.pop();
    }
  }

  async function handleSlashCommand(raw: string) {
    try {
      const result = await executeCommand(raw, props.runtime);

      if (result.openModelPicker) {
        openModelPalette(result.openModelPicker.models);
      }
      if (result.openThinkingPicker) {
        openThinkingPalette(
          result.openThinkingPicker.levels,
          result.openThinkingPicker.current,
        );
      }
      if (result.openNameInput) {
        openNameInputPalette(result.openNameInput.currentName);
      }
      if (result.openSessionPicker) {
        openSessionSwitchPalette(result.openSessionPicker.sessions);
      }
      if (result.openSessionManage) {
        openSessionManagePalette(
          result.openSessionManage.sessions,
          result.openSessionManage.currentSessionId!,
        );
      }
    } catch (error) {
      console.error(error);
    }
  }

  function openThinkingPalette(levels: string[], current: string) {
    const options = levels.map((level) => ({
      name: level,
      description: level === current ? "(current)" : "",
      value: level,
      action: (ctx: { dismiss: () => void }) => {
        props.runtime.setThinkingLevel(
          level as import("@mariozechner/pi-agent-core").ThinkingLevel,
        );
        ctx.dismiss();
      },
    }));
    props.palette.show({ options });
  }

  function openNameInputPalette(currentName: string) {
    props.palette.show({
      mode: "input",
      label: "Session name",
      inputValue: currentName,
      onSubmit: (value, ctx) => {
        if (value.trim()) {
          props.runtime.setSessionName(value.trim());
        }
        ctx.dismiss();
      },
    });
  }

  function openModelPalette(
    models: Array<{ id: string; name: string; provider: string }>,
  ) {
    const options = models.map((m) => ({
      name: m.name,
      description: m.provider,
      value: m,
      action: async (ctx: { dismiss: () => void }) => {
        try {
          await props.runtime.setModel(m.provider, m.id);
        } catch (error) {
          console.error(error);
        }
        ctx.dismiss();
      },
    }));
    props.palette.show({ options, filterable: true });
  }

  function openSessionSwitchPalette(
    sessions: Array<{
      path: string;
      id: string;
      name: string | undefined;
      cwd: string;
      modified: Date;
      firstMessage: string;
    }>,
  ) {
    const home = process.env.HOME || process.env.USERPROFILE || "";
    const options = sessions.map((s) => {
      const label = s.name || s.firstMessage.slice(0, 60) || s.id.slice(0, 8);
      const cwd = s.cwd.startsWith(home)
        ? `~${s.cwd.slice(home.length)}`
        : s.cwd;
      const dir = cwd.split("/").pop() || cwd;
      const ago = formatTimeAgo(s.modified);
      return {
        name: label,
        description: `${dir}  ${ago}`,
        value: s,
        action: async (ctx: { dismiss: () => void }) => {
          try {
            await props.runtime.switchSession(s.path);
          } catch (error) {
            console.error(error);
          }
          ctx.dismiss();
        },
      };
    });
    props.palette.show({ options, filterable: true });
  }

  function openSessionManagePalette(
    sessions: Array<{
      path: string;
      id: string;
      name: string | undefined;
      cwd: string;
      modified: Date;
      firstMessage: string;
    }>,
    currentSessionId: string,
  ) {
    let manageSessions = [...sessions];

    function buildOptions() {
      const home = process.env.HOME || process.env.USERPROFILE || "";
      return manageSessions.map((s) => {
        const label = s.name || s.firstMessage.slice(0, 60) || s.id.slice(0, 8);
        const cwd = s.cwd.startsWith(home)
          ? `~${s.cwd.slice(home.length)}`
          : s.cwd;
        const dir = cwd.split("/").pop() || cwd;
        const ago = formatTimeAgo(s.modified);
        return {
          name: label,
          description: `${dir}  ${ago}`,
          value: s,
          action: () => {},
        };
      });
    }

    function refresh() {
      props.palette.pop();
      props.palette.show(
        {
          options: buildOptions(),
          filterable: true,
          hint: "Ctrl+R rename · Ctrl+D delete · Esc close",
        },
        {
          "ctrl+r": (option, _ctx) => {
            const session = option.value as {
              path: string;
              name: string | undefined;
              id: string;
            };
            props.palette.show({
              mode: "input",
              label: "Rename session",
              inputValue: session.name || "",
              onSubmit: (value, inputCtx) => {
                try {
                  props.runtime.renameSession(session.path, value);
                  session.name = value;
                } catch (error) {
                  console.error(error);
                }
                inputCtx.dismiss();
                refresh();
              },
            });
          },
          "ctrl+d": async (option, _ctx) => {
            const session = option.value as { path: string; id: string };
            if (session.id === currentSessionId) {
              return;
            }
            try {
              await props.runtime.deleteSession(session.path);
              manageSessions = manageSessions.filter(
                (s) => s.id !== session.id,
              );
              refresh();
            } catch (error) {
              console.error(error);
            }
          },
        },
      );
    }

    refresh();
  }

  function formatTimeAgo(date: Date): string {
    const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
    if (seconds < 60) return "just now";
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 30) return `${days}d ago`;
    return date.toLocaleDateString();
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

    // Enter selects (filterable pickers)
    if (pm.isFilterable && e.name === "return") {
      e.preventDefault();
      filterText = "";
      pm.selectCurrent();
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

    if (text.trimStart().startsWith("/")) {
      textareaRef?.setText("");
      await handleSlashCommand(text.trim());
      return;
    }

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
          showCursor={!props.palette.isFilterable && !props.palette.isInputMode}
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
          onKeyDown={(e) => {
            console.log(`pressed: ${e.name}`);
          }}
          focused
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
