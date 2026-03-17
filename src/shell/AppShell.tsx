import { createSignal, Show } from "solid-js";
import type { PagerController } from "../features/pager";
import type { WizardController } from "../features/wizard";
import type { AppState } from "../state/app-state";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import type { ComposerController } from "./composer-controller";
import { InlinePicker } from "./InlinePicker";
import { PagerView } from "./PagerView";
import { PendingSlot } from "./PendingSlot";
import { TranscriptPane } from "./TranscriptPane";
import { WizardDock } from "./WizardDock";
import { WizardView } from "./WizardView";
import { theme } from "./theme";

const STATUS_BAR_HEIGHT = 1;

export type AppShellProps = {
  state: AppState;
  controller: ComposerController;
  pager: PagerController;
  wizard: WizardController;
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
      {/* Main content area — transcript, pager, or wizard */}
      <Show when={props.wizard.active}>
        <WizardView wizard={props.wizard} />
      </Show>
      <Show when={props.pager.active && !props.wizard.active}>
        <PagerView pager={props.pager} />
      </Show>
      <Show when={!props.pager.active && !props.wizard.active}>
        <TranscriptPane messages={props.state.messages} />
      </Show>

      {/* Dock area — composer or wizard input */}
      <box flexShrink={0} flexDirection="column" gap={0}>
        <PendingSlot panel={props.state.panel} />
        <Show when={props.wizard.active}>
          <WizardDock wizard={props.wizard} />
        </Show>
        <Show when={!props.wizard.active}>
          <ComposerDock
            cwd={props.state.footerStatus.cwd}
            sessionName={props.state.sessionMeta.sessionName}
            controller={props.controller}
            pager={props.pager}
            onHeightChange={setDockHeight}
          />
        </Show>
        <BottomStatusBar status={props.state.footerStatus} />
      </box>

      {/* Picker overlay (only when not in wizard mode) */}
      <Show when={!props.wizard.active}>
        <InlinePicker
          palette={props.controller.palette}
          bottomOffset={dockHeight() + STATUS_BAR_HEIGHT + 2}
        />
      </Show>
    </box>
  );
}
