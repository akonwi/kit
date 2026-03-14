import type { AppState } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import { PanelHost } from "./PanelHost";
import { TranscriptPane } from "./TranscriptPane";

export type AppShellProps = {
  state: AppState;
};

export function AppShell(props: AppShellProps) {
  return (
    <box width="100%" height="100%" flexDirection="column">
      <TranscriptPane items={props.state.transcript} />
      <box flexShrink={0} flexDirection="column" gap={0}>
        <PanelHost panel={props.state.panel} />
        <ComposerDock composer={props.state.composer} cwd={props.state.footerStatus.cwd} sessionName={props.state.sessionMeta.sessionName} />
        <BottomStatusBar status={props.state.footerStatus} />
      </box>
    </box>
  );
}
