import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { AssistantMessage } from "@mariozechner/pi-ai";
import type { AgentRuntime } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import { setNotificationConfigRef } from "../features/commands/bells-speech";
import { loadNotificationConfig, saveNotificationConfig, type NotificationConfig } from "../features/notification-config";
import { ringBell, speak } from "../features/notifications";
import { createPagerController } from "../features/pager";
import type { WizardController } from "../features/wizard";
import { AppShell } from "../shell/AppShell";
import { createComposerController } from "../shell/composer-controller";
import { createAppState } from "../state/app-state";

export type AppProps = {
  settings: LoadedSettings;
  session: LoadedSession | null;
  runtime: AgentRuntime;
  wizard: WizardController;
  notificationConfig: NotificationConfig;
};

function findLastAssistant(messages: AgentMessage[]): AssistantMessage | null {
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i];
    if ("role" in msg && msg.role === "assistant") return msg as AssistantMessage;
  }
  return null;
}

function extractAssistantText(msg: AssistantMessage): string {
  return msg.content
    .filter((b): b is { type: "text"; text: string } => b.type === "text" && "text" in b && typeof b.text === "string")
    .map((b) => b.text)
    .join("\n")
    .trim();
}

export function App(props: AppProps) {
  const app = createAppState(props.settings, props.runtime.getSession(), props.runtime);
  const pager = createPagerController(props.runtime);

  // Notification config — mutable ref shared with /bells and /speech commands
  const configRef = { current: props.notificationConfig };
  setNotificationConfigRef(configRef, (config) => {
    app.setNotificationStatus(config.bells.enabled, config.speech.enabled);
    saveNotificationConfig(config).catch(() => {});
  });

  // Set initial notification status in footer
  app.setNotificationStatus(configRef.current.bells.enabled, configRef.current.speech.enabled);

  const controller = createComposerController({
    runtime: props.runtime,
    fileIndex: app.fileIndex,
    threadIndex: app.threadIndex,
    pager,
    addNotice: app.addNotice,
  });

  // Auto-activate pager when agent finishes a turn with 2+ sections
  props.runtime.subscribe((event) => {
    if (event.type === "turn_complete" && !pager.active && !props.wizard.active) {
      pager.tryActivate(event.messages);
    }

    // Bells and speech on turn complete
    if (event.type === "turn_complete") {
      const config = configRef.current;
      const lastAssistant = findLastAssistant(event.messages);
      const isError = lastAssistant != null && (
        lastAssistant.stopReason === "error" ||
        lastAssistant.stopReason === "aborted"
      );

      if (config.bells.enabled) {
        ringBell(isError);
      }

      if (config.speech.enabled && lastAssistant && !isError) {
        const text = extractAssistantText(lastAssistant);
        const sessionId = props.runtime.getSession().sessionId;
        speak(text, sessionId, {
          voice: config.speech.voice ?? undefined,
          maxChars: config.speech.maxChars,
        });
      }

      // Sync notification status to footer and persist config
      app.setNotificationStatus(config.bells.enabled, config.speech.enabled);
      saveNotificationConfig(config).catch(() => {});
    }
  });

  return (
    <AppShell
      state={app.state}
      controller={controller}
      pager={pager}
      wizard={props.wizard}
    />
  );
}
