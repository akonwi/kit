import type { AppState } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import { PendingSlot } from "./PendingSlot";
import { TranscriptPane } from "./TranscriptPane";
import { theme } from "./theme";

export type AppShellProps = {
  state: AppState;
  controller: ComposerController;
};

export function AppShell(props: AppShellProps) {
  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.bg}
    >
      <TranscriptPane messages={props.state.messages} />
      <box flexShrink={0} flexDirection="column" gap={0}>
        <PendingSlot panel={props.state.panel} />
        <ComposerDock
          cwd={props.state.footerStatus.cwd}
          sessionName={props.state.sessionMeta.sessionName}
          controller={props.controller}
        />
        <BottomStatusBar status={props.state.footerStatus} />
      </box>
    </box>
  );
}
