import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import { AppShell } from "../shell/AppShell";
import { createAppState } from "../state/app-state";

export type AppProps = {
  settings: LoadedSettings;
  session: LoadedSession | null;
};

export function App(props: AppProps) {
  const appState = createAppState(props.settings, props.session);
  return <AppShell state={appState.state} />;
}
