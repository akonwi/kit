import type { AgentRuntime } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import { AppShell } from "../shell/AppShell";
import { createComposerController } from "../shell/composer-controller";
import { createAppState } from "../state/app-state";

export type AppProps = {
  settings: LoadedSettings;
  session: LoadedSession | null;
  runtime: AgentRuntime;
};

export function App(props: AppProps) {
  const app = createAppState(props.settings, props.runtime.getSession(), props.runtime);

  const controller = createComposerController({
    runtime: props.runtime,
    fileIndex: app.fileIndex,
    threadIndex: app.threadIndex,
  });

  return (
    <AppShell
      state={app.state}
      controller={controller}
    />
  );
}
