import { createSignal, onCleanup, Show } from "solid-js";
import type { PanelState } from "../state/app-state";
import { theme } from "./theme";

const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
const SPINNER_INTERVAL = 80;

export type PanelHostProps = {
  panel: PanelState;
};

function panelMessage(panel: PanelState): string {
  if (!panel.visible) return "";
  return panel.lines.filter(Boolean).join(" ").trim();
}

function Spinner() {
  const [frame, setFrame] = createSignal(0);
  const timer = setInterval(() => {
    setFrame((f) => (f + 1) % SPINNER_FRAMES.length);
  }, SPINNER_INTERVAL);
  onCleanup(() => clearInterval(timer));

  return <text fg={theme.panelText}>{SPINNER_FRAMES[frame()]}</text>;
}

export function PanelHost(props: PanelHostProps) {
  return (
    <box flexShrink={0} height={1} paddingLeft={1} paddingRight={1} flexDirection="row" gap={1}>
      <Show when={props.panel.visible}>
        <Spinner />
      </Show>
      <text fg={theme.panelText}>{panelMessage(props.panel)}</text>
    </box>
  );
}
