import { homedir } from "node:os";
import type { AgentMessage } from "@mariozechner/pi-agent-core";
import { createStore } from "solid-js/store";
import type { AgentRuntime, RuntimeStatus } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import { matchCommands } from "../features/command-registry";
import { executeCommand } from "../features/commands";

export type DockMode = "composer" | "wizard" | "pager";

export type PanelState = {
  visible: boolean;
  title: string;
  lines: string[];
};

export type ComposerState = {
  mode: DockMode;
  title: string;
  placeholder: string;
  text: string;
  height: number;
};

export type FooterStatusState = {
  cwd: string;
  model: string;
  thinkingLevel: string;
  contextPct: string;
};

export type SessionMeta = {
  sessionId: string;
  sessionName: string | undefined;
  sessionCwd: string;
  hasSession: boolean;
};

export type PickerOption = {
  name: string;
  description: string;
  value: unknown;
};


export type PickerState = {
  visible: boolean;
  title: string;
  options: PickerOption[];
  selectedIndex: number;
  filterable: boolean;
  filterText: string;
};

export type AppState = {
  messages: AgentMessage[];
  panel: PanelState;
  picker: PickerState;
  composer: ComposerState;
  footerStatus: FooterStatusState;
  sessionMeta: SessionMeta;
  /** Debug: raw message JSON for the currently inspected message */
  debugEntry: string | null;
};

function formatCwd(rawCwd: string): string {
  const home = homedir();
  return rawCwd.startsWith(home) ? `~${rawCwd.slice(home.length)}` : rawCwd;
}

function deriveFooterStatus(runtime: AgentRuntime | null): Omit<FooterStatusState, "cwd"> {
  if (runtime) {
    const status = runtime.getStatus();
    return {
      model: status.model,
      thinkingLevel: status.thinkingLevel,
      contextPct: status.contextPct,
    };
  }
  return {
    model: "no-model",
    thinkingLevel: "off",
    contextPct: "–",
  };
}

function applyRuntimeStatus(
  current: FooterStatusState,
  status: RuntimeStatus,
): FooterStatusState {
  return {
    ...current,
    model: status.model,
    thinkingLevel: status.thinkingLevel,
    contextPct: status.contextPct,
  };
}

function buildSessionMeta(session: LoadedSession | null): SessionMeta {
  if (session) {
    return {
      sessionId: session.sessionId,
      sessionName: session.sessionName,
      sessionCwd: session.cwd,
      hasSession: true,
    };
  }
  return {
    sessionId: "",
    sessionName: undefined,
    sessionCwd: process.cwd(),
    hasSession: false,
  };
}

export function buildInitialAppState(
  _settings: LoadedSettings,
  session: LoadedSession | null,
  runtime: AgentRuntime | null,
): AppState {
  const messages = runtime ? runtime.getMessages() : [];
  const footer = deriveFooterStatus(runtime);

  return {
    messages,
    panel: {
      visible: false,
      title: "",
      lines: [],
    },
    picker: {
      visible: false,
      title: "",
      options: [],
      selectedIndex: 0,
      filterable: false,
      filterText: "",
    },
    composer: {
      mode: "composer",
      title: "Compose",
      placeholder: "Ask pi-kit to do something...",
      text: "",
      height: 6,
    },
    footerStatus: {
      cwd: formatCwd(process.cwd()),
      ...footer,
    },
    sessionMeta: buildSessionMeta(session),
    debugEntry: null,
  };
}

export function createAppState(
  settings: LoadedSettings,
  session: LoadedSession | null,
  runtime: AgentRuntime | null,
) {
  const [state, setState] = createStore(buildInitialAppState(settings, session, runtime));

  function showPanel(title: string, lines: string[]) {
    setState("panel", {
      visible: true,
      title,
      lines,
    });
  }

  function hidePanel() {
    setState("panel", {
      visible: false,
      title: "",
      lines: [],
    });
  }

  runtime?.subscribe((event) => {
    switch (event.type) {
      case "messages_changed":
        setState("messages", event.messages);
        break;
      case "status_changed":
        setState("footerStatus", applyRuntimeStatus(state.footerStatus, event.status));
        break;
      case "panel":
        setState("panel", event.panel);
        break;
      case "error":
        showPanel(event.title, event.lines);
        break;
      default:
        break;
    }
  });

  function inspectMessage(msg: AgentMessage) {
    const json = JSON.stringify(msg, null, 2);
    const current = state.debugEntry;
    setState("debugEntry", current === json ? null : json);
  }

  function setComposerText(text: string) {
    setState("composer", "text", text);
    updateCommandPicker(text);
  }

  function updateCommandPicker(text: string) {
    const trimmed = text.trimStart();

    // Only show command picker if text is purely a slash prefix (no space yet = still picking)
    if (trimmed.startsWith("/") && !trimmed.includes(" ")) {
      const matches = matchCommands(trimmed);
      if (matches.length > 0) {
        const options: PickerOption[] = matches.map((c) => ({
          name: c.name,
          description: c.description,
          value: c,
        }));
        openPicker("Commands", options, 0, (option) => {
          const cmd = option.value as { name: string; takesArgs?: boolean };
          closePicker();
          if (cmd.takesArgs) {
            // Fill in the command and let user type args
            setState("composer", "text", `${cmd.name} `);
          } else {
            // Execute immediately
            setState("composer", "text", cmd.name);
            handleSlashCommand(cmd.name);
          }
        });
        return;
      }
    }

    // Dismiss the command picker if it's currently showing commands
    if (state.picker.visible && state.picker.title === "Commands") {
      dismissPicker();
    }
  }

  async function handleSlashCommand(raw: string) {
    if (!runtime) {
      showPanel("", ["No runtime available."]);
      setState("composer", "text", "");
      return;
    }

    setState("composer", "text", "");

    try {
      const result = await executeCommand(raw, runtime);
      if (result.panel) {
        showPanel(result.panel.title, result.panel.lines);
      }
      if (result.sessionName !== undefined) {
        setState("sessionMeta", "sessionName", result.sessionName);
      }
      if (result.openModelPicker) {
        const { models, currentModelId } = result.openModelPicker;
        const options: PickerOption[] = models.map((m) => ({
          name: m.name,
          description: m.provider,
          value: m,
        }));
        const currentIdx = models.findIndex((m) => m.id === currentModelId);
        openPicker(
          "Select Model",
          options,
          currentIdx >= 0 ? currentIdx : 0,
          async (option: PickerOption) => {
            const model = option.value as { id: string; name: string; provider: string };
            try {
              await runtime.setModel(model.provider, model.id);
              hidePanel();
            } catch (error) {
              if (error instanceof Error) {
                showPanel("Model Error", [error.message]);
              }
            }
            closePicker();
          },
          true, // filterable
        );
      }
    } catch (error) {
      if (error instanceof Error) {
        showPanel("Command Error", [error.message]);
      } else {
        showPanel("Command Error", [String(error)]);
      }
    }
  }

  async function submitComposer() {
    const raw = state.composer.text;
    if (!raw.trim()) return;

    if (raw.trimStart().startsWith("/")) {
      if (state.picker.visible && state.picker.title === "Commands") {
        closePicker();
      }
      handleSlashCommand(raw);
      return;
    }

    if (!runtime) {
      showPanel("Runtime Error", ["No runtime is available for this submission."]);
      return;
    }

    setState("composer", "text", "");

    try {
      await runtime.submitUserMessage(raw);
    } catch (error) {
      setState("composer", "text", raw);
      if (error instanceof Error) {
        showPanel("Runtime Error", [error.message]);
      } else {
        showPanel("Runtime Error", [String(error)]);
      }
    }
  }

  let pickerCallback: ((option: PickerOption) => void) | null = null;
  let pickerAllOptions: PickerOption[] = [];

  function openPicker(
    title: string,
    options: PickerOption[],
    selectedIndex: number,
    onSelect: (option: PickerOption) => void,
    filterable = false,
  ) {
    pickerCallback = onSelect;
    pickerAllOptions = options;
    setState("picker", {
      visible: true,
      title,
      options,
      selectedIndex,
      filterable,
      filterText: "",
    });
  }

  function closePicker() {
    pickerCallback = null;
    pickerAllOptions = [];
    setState("picker", {
      visible: false,
      title: "",
      options: [],
      selectedIndex: 0,
      filterable: false,
      filterText: "",
    });
    setState("composer", "text", "");
  }

  /** Dismiss picker without clearing the composer text */
  function dismissPicker() {
    pickerCallback = null;
    pickerAllOptions = [];
    setState("picker", {
      visible: false,
      title: "",
      options: [],
      selectedIndex: 0,
      filterable: false,
      filterText: "",
    });
  }

  function filterPicker(query: string) {
    if (!state.picker.visible || !state.picker.filterable) return;
    setState("picker", "filterText", query);
    if (!query) {
      setState("picker", "options", pickerAllOptions);
      setState("picker", "selectedIndex", 0);
      return;
    }
    const q = query.toLowerCase();
    const filtered = pickerAllOptions.filter(
      (o) => o.name.toLowerCase().includes(q) || o.description.toLowerCase().includes(q),
    );
    setState("picker", "options", filtered);
    setState("picker", "selectedIndex", 0);
  }

  function selectPickerOption(option: PickerOption) {
    pickerCallback?.(option);
  }

  function selectCurrentPickerOption() {
    const option = state.picker.options[state.picker.selectedIndex];
    if (option) pickerCallback?.(option);
  }

  function pickerUp() {
    const count = state.picker.options.length;
    if (count === 0) return;
    setState("picker", "selectedIndex", (i) => (i <= 0 ? count - 1 : i - 1));
  }

  function pickerDown() {
    const count = state.picker.options.length;
    if (count === 0) return;
    setState("picker", "selectedIndex", (i) => (i >= count - 1 ? 0 : i + 1));
  }

  return {
    state,
    setState,
    inspectMessage,
    setComposerText,
    submitComposer,
    showPanel,
    hidePanel,
    openPicker,
    closePicker,
    selectPickerOption,
    selectCurrentPickerOption,
    pickerUp,
    pickerDown,
    filterPicker,
  };
}
