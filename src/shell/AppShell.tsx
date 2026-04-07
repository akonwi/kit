import { createSignal } from "solid-js";
import { useRenderer } from "@opentui/solid";
import type { AppState } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import { InlinePicker } from "./InlinePicker";
import { PendingSlot } from "./PendingSlot";
import { copySelection } from "./selection";
import { TranscriptPane } from "./TranscriptPane";
import { theme } from "./theme";

const STATUS_BAR_HEIGHT = 1;

export type AppShellProps = {
  state: AppState;
  controller: ComposerController;
};

export function AppShell(props: AppShellProps) {
  const [dockHeight, setDockHeight] = createSignal(3);
  const renderer = useRenderer();

  return (
    <box
      width="100%"
      height="100%"
      flexDirection="column"
      backgroundColor={theme.bg}
      onMouseUp={() => copySelection(renderer)}
    >
      <TranscriptPane messages={props.state.messages} notices={props.state.notices} />

      <box flexShrink={0} flexDirection="column" gap={0}>
        <PendingSlot panel={props.state.panel} />
        <ComposerDock
          cwd={props.state.footerStatus.cwd}
          sessionName={props.state.sessionMeta.sessionName}
          gitBranch={props.state.footerStatus.gitBranch}
          gitDirty={props.state.footerStatus.gitDirty}
          controller={props.controller}
          onHeightChange={setDockHeight}
        />
        <BottomStatusBar status={props.state.footerStatus} />
      </box>

      <InlinePicker
        palette={props.controller.palette}
        bottomOffset={dockHeight() + STATUS_BAR_HEIGHT + 2}
      />
    </box>
  );
}
