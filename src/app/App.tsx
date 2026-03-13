import type { LoadedSettings } from "../compat/settings/load-settings";
import { AppShell } from "../shell/AppShell";

export type AppProps = {
  settings: LoadedSettings;
};

export function App({ settings }: AppProps) {
  return <AppShell settings={settings} />;
}
