import type { FooterStatusState } from "../state/app-state";
import { theme } from "./theme";

const BLOCK_CHARS = "▁▂▃▄▅▆▇█";

function contextBlock(pct: string): string {
  const n = parseInt(pct, 10);
  if (isNaN(n)) return "▁";
  const idx = Math.min(Math.floor((n / 100) * BLOCK_CHARS.length), BLOCK_CHARS.length - 1);
  return BLOCK_CHARS[idx];
}

export type BottomStatusBarProps = {
  status: FooterStatusState;
};

export function BottomStatusBar(props: BottomStatusBarProps) {
  const bell = () => props.status.bellsEnabled ? "🔔" : "🔕";
  const speech = () => props.status.speechEnabled ? "🗣" : "🤫";

  return (
    <box
      flexShrink={0}
      border
      borderColor={theme.borderStatus}
      paddingX={1}
      flexDirection="row"
      justifyContent="space-between"
    >
      <text fg={theme.textMuted}>
        {props.status.model} ({props.status.thinkingLevel}) {contextBlock(props.status.contextPct)}{props.status.contextPct}  {bell()} {speech()}
      </text>
    </box>
  );
}
