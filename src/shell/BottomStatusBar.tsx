import type { FooterStatusState } from "../state/app-state";

export type BottomStatusBarProps = {
  status: FooterStatusState;
};

export function BottomStatusBar(props: BottomStatusBarProps) {
  const row1Left = `${props.status.model} (${props.status.thinkingLevel}) 🪟${props.status.contextPct}`;
  const row1Right = `${props.status.bellStatus}  ${props.status.speechStatus}`;

  return (
    <box
      flexShrink={0}
      border
      borderColor="#5d5330"
      paddingX={1}
      flexDirection="column"
    >
      <box flexDirection="row" justifyContent="space-between">
        <text fg="#8f8f8f">{row1Left}</text>
        <text fg="#8f8f8f">{row1Right}</text>
        <text fg="#8f8f8f">{props.status.repoSummary}</text>
      </box>
    </box>
  );
}
