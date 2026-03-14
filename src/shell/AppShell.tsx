import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type { AppState } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import { DebugPanel } from "./DebugPanel";
import { PanelHost } from "./PanelHost";
import { TranscriptPane } from "./TranscriptPane";

export type AppShellProps = {
  state: AppState;
  onInspectMessage: (msg: AgentMessage) => void;
  onComposerChange: (value: string) => void;
  onComposerSubmit: () => void;
};

export function AppShell(props: AppShellProps) {
  return (
    <box width="100%" height="100%" flexDirection="column">
      <TranscriptPane messages={props.state.messages} onMessageClick={props.onInspectMessage} />
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
