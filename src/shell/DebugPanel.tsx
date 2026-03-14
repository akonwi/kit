import { Show } from "solid-js";

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
        borderColor="#8a6bbd"
        padding={1}
      >
        <box flexDirection="column" width="100%">
          <text fg="#8a6bbd">Debug: Raw Message</text>
          <text fg="#c0c0c0">{props.json}</text>
        </box>
      </scrollbox>
    </Show>
  );
}
