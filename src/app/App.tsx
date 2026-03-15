import type { AgentRuntime } from "../backend";
import type { LoadedSession } from "../compat/sessions";
import type { LoadedSettings } from "../compat/settings/load-settings";
import { AppShell } from "../shell/AppShell";
import { createAppState } from "../state/app-state";

export type AppProps = {
  settings: LoadedSettings;
  session: LoadedSession | null;
  runtime: AgentRuntime;
};

export function App(props: AppProps) {
  const appState = createAppState(props.settings, props.runtime.getSession(), props.runtime);
  return (
    <AppShell
      state={appState.state}
      onInspectMessage={appState.inspectMessage}
      onComposerChange={appState.setComposerText}
      onComposerSubmit={appState.submitComposer}
      onPickerSelect={appState.selectPickerOption}
      onPickerSelectCurrent={appState.selectCurrentPickerOption}
      onPickerUp={appState.pickerUp}
      onPickerDown={appState.pickerDown}
      onPickerDismiss={appState.closePicker}
    />
  );
}
