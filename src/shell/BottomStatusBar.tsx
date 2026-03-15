import type { FooterStatusState } from "../state/app-state";

export type BottomStatusBarProps = {
  status: FooterStatusState;
};

export function BottomStatusBar(props: BottomStatusBarProps) {
  return (
    <box
      flexShrink={0}
      border
      borderColor="#5d5330"
      paddingX={1}
      flexDirection="row"
      justifyContent="space-between"
    >
      <text fg="#8f8f8f">
        {props.status.model} ({props.status.thinkingLevel}) 🪟{props.status.contextPct}
      </text>
    </box>
  );
}
