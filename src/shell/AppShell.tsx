import type { AppState, TranscriptItem } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import { DebugPanel } from "./DebugPanel";
import { PanelHost } from "./PanelHost";
import { TranscriptPane } from "./TranscriptPane";

export type AppShellProps = {
  state: AppState;
  onInspectItem: (item: TranscriptItem) => void;
  onComposerChange: (value: string) => void;
  onComposerSubmit: () => void;
};

export function AppShell(props: AppShellProps) {
  return (
    <box width="100%" height="100%" flexDirection="column">
      <TranscriptPane items={props.state.transcript} onItemClick={props.onInspectItem} />
      <box flexShrink={0} flexDirection="column" gap={0}>
        <PanelHost panel={props.state.panel} />
        <DebugPanel json={props.state.debugEntry} />
        <ComposerDock
          composer={props.state.composer}
          cwd={props.state.footerStatus.cwd}
          sessionName={props.state.sessionMeta.sessionName}
          onContentChange={props.onComposerChange}
          onSubmit={props.onComposerSubmit}
        />
        <BottomStatusBar status={props.state.footerStatus} />
      </box>
    </box>
  );
}
