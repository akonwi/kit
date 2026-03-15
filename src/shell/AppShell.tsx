import type { AppState } from "../state/app-state";
import type { PaletteManager } from "../state/palette-manager";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import { InlinePicker } from "./InlinePicker";
import { PendingSlot } from "./PendingSlot";
import { TranscriptPane } from "./TranscriptPane";
import { theme } from "./theme";

export type AppShellProps = {
  state: AppState;
  palette: PaletteManager;
  onComposerTextChange: (text: string) => void;
  onComposerSubmit: (text: string) => Promise<{ composerText?: string }>;
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
          palette={props.palette}
          onTextChange={props.onComposerTextChange}
          onSubmit={props.onComposerSubmit}
        />
        <BottomStatusBar status={props.state.footerStatus} />
      </box>
      <InlinePicker palette={props.state.palette} />
    </box>
  );
}
