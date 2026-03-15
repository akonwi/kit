import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { AppState, PickerOption } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import { DebugPanel } from "./DebugPanel";
import { InlinePicker } from "./InlinePicker";
import { PanelHost } from "./PanelHost";
import { TranscriptPane } from "./TranscriptPane";

export type AppShellProps = {
  state: AppState;
  onInspectMessage: (msg: AgentMessage) => void;
  onComposerChange: (value: string) => void;
  onComposerSubmit: () => void;
  onPickerSelect: (option: PickerOption) => void;
  onPickerSelectCurrent: () => void;
  onPickerUp: () => void;
  onPickerDown: () => void;
  onPickerDismiss: () => void;
  onPickerFilter: (query: string) => void;
};

export function AppShell(props: AppShellProps) {
  return (
    <box width="100%" height="100%" flexDirection="column">
      <TranscriptPane messages={props.state.messages} onMessageClick={props.onInspectMessage} />
      <box flexShrink={0} flexDirection="column" gap={0}>
        <PanelHost panel={props.state.panel} />
        <DebugPanel json={props.state.debugEntry} />
        <InlinePicker picker={props.state.picker} />
        <ComposerDock
          composer={props.state.composer}
          cwd={props.state.footerStatus.cwd}
          sessionName={props.state.sessionMeta.sessionName}
          pickerVisible={props.state.picker.visible}
          pickerFilterable={props.state.picker.filterable}
          onContentChange={props.onComposerChange}
          onSubmit={props.onComposerSubmit}
          onPickerUp={props.onPickerUp}
          onPickerDown={props.onPickerDown}
          onPickerSelect={props.onPickerSelectCurrent}
          onPickerDismiss={props.onPickerDismiss}
          onPickerFilter={props.onPickerFilter}
        />
        <BottomStatusBar status={props.state.footerStatus} />
      </box>
    </box>
  );
}
