import type { AgentRuntime } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import type { FileIndex } from "../features/files";
import { AppShell } from "../shell/AppShell";
import { createAppState } from "../state/app-state";

export type AppProps = {
  settings: LoadedSettings;
  session: LoadedSession | null;
  runtime: AgentRuntime;
};

export function App(props: AppProps) {
  const app = createAppState(props.settings, props.runtime.getSession(), props.runtime);
  return (
    <AppShell
      state={app.state}
      palette={app.palette}
      runtime={props.runtime}
      fileIndex={app.fileIndex}
    />
  );
}
