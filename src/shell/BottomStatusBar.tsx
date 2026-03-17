import type { FooterStatusState } from "../state/app-state";
import { theme } from "./theme";

export type BottomStatusBarProps = {
  status: FooterStatusState;
};

export function BottomStatusBar(props: BottomStatusBarProps) {
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
        {props.status.model} ({props.status.thinkingLevel}) 🪟{props.status.contextPct}
      </text>
    </box>
  );
}
