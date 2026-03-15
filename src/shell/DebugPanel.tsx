import { Show } from "solid-js";
import { theme } from "./theme";

export type DebugPanelProps = {
  json: string | null;
};

export function DebugPanel(props: DebugPanelProps) {
  return (
    <Show when={props.json}>
      <scrollbox
        flexShrink={0}
        height={16}
        scrollY
        border
        borderColor={theme.borderDebug}
        padding={1}
      >
        <box flexDirection="column" width="100%">
          <text fg={theme.debugLabel}>Debug: Raw Message</text>
          <text fg={theme.textDebug}>{props.json}</text>
        </box>
      </scrollbox>
    </Show>
  );
}
