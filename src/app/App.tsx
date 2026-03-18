import type { AgentRuntime } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import { setNotificationConfigRef } from "../features/commands/bells-speech";
import { saveNotificationConfig, type NotificationConfig } from "../features/notification-config";
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
  });

  // Auto-activate pager when agent finishes a turn with 2+ sections
  props.runtime.subscribe((event) => {
    if (event.type === "turn_complete" && !pager.active && !props.wizard.active) {
      pager.tryActivate(event.messages);
    }

    // Sync notification status on turn complete (actual bells/speech
    // are handled by the pi-kit extension loaded via pi-coding-agent)
    if (event.type === "turn_complete") {
      app.setNotificationStatus(configRef.current.bells.enabled, configRef.current.speech.enabled);
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
