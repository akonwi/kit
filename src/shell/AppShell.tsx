import { createSignal, Show } from "solid-js";
import type { PagerController } from "../features/pager";
import type { AppState } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import { InlinePicker } from "./InlinePicker";
import { PagerView } from "./PagerView";
import { PendingSlot } from "./PendingSlot";
import { TranscriptPane } from "./TranscriptPane";
import { theme } from "./theme";

const STATUS_BAR_HEIGHT = 1;

export type AppShellProps = {
  state: AppState;
  controller: ComposerController;
  pager: PagerController;
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
      <Show when={!props.pager.active}>
        <TranscriptPane messages={props.state.messages} />
      </Show>
      <Show when={props.pager.active}>
        <PagerView pager={props.pager} />
      </Show>
      <box flexShrink={0} flexDirection="column" gap={0}>
        <PendingSlot panel={props.state.panel} />
        <ComposerDock
          cwd={props.state.footerStatus.cwd}
          sessionName={props.state.sessionMeta.sessionName}
          controller={props.controller}
          pager={props.pager}
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
