import type { PanelState } from "../state/app-state";

export type PanelHostProps = {
  panel: PanelState;
};

function renderPanelLine(panel: PanelState): string {
  if (!panel.visible) return "";
  const message = panel.lines.filter(Boolean).join(" ").trim();
  if (!panel.title) return message;
  if (!message) return panel.title;
  return `${panel.title}: ${message}`;
}

export function PanelHost(props: PanelHostProps) {
  return (
    <box flexShrink={0} height={1} paddingLeft={1} paddingRight={1} flexDirection="row">
      <text fg="#8f8f8f">{renderPanelLine(props.panel)}</text>
    </box>
  );
}
