import type { AgentRuntime } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
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
};

export function App(props: AppProps) {
  const app = createAppState(props.settings, props.runtime.getSession(), props.runtime);
  const pager = createPagerController(props.runtime);

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
