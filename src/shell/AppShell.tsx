import { createSignal } from "solid-js";
import type { AgentRuntime } from "../backend";
import type { FileIndex } from "../features/files";
import type { ThreadIndex } from "../features/threads";
import type { AppState } from "../state/app-state";
import type { PaletteManager } from "../state/palette-manager";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import { InlinePicker } from "./InlinePicker";
import { PendingSlot } from "./PendingSlot";
import { TranscriptPane } from "./TranscriptPane";
import { theme } from "./theme";

const STATUS_BAR_HEIGHT = 1;

export type AppShellProps = {
  state: AppState;
  palette: PaletteManager;
  runtime: AgentRuntime;
  fileIndex: FileIndex;
  threadIndex: ThreadIndex | null;
};

export function AppShell(props: AppShellProps) {
  const [dockHeight, setDockHeight] = createSignal(3);

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
          palette={props.palette}
          runtime={props.runtime}
          fileIndex={props.fileIndex}
          threadIndex={props.threadIndex}
          onHeightChange={setDockHeight}
        />
        <BottomStatusBar status={props.state.footerStatus} />
      </box>
      <InlinePicker
        palette={props.palette}
        bottomOffset={
          dockHeight() + STATUS_BAR_HEIGHT + 2 /* extra for borders */
        }
      />
    </box>
  );
}
