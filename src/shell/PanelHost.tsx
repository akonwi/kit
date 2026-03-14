import { For, Show } from "solid-js";
import type { PanelState } from "../state/app-state";

export type PanelHostProps = {
  panel: PanelState;
};

export function PanelHost(props: PanelHostProps) {
  return (
    <Show when={props.panel.visible}>
      <box
        flexShrink={0}
        border
        borderColor="#2f6e9b"
        paddingX={1}
        flexDirection="column"
        gap={1}
      >
        <text fg="#6cb6ff">{props.panel.title}</text>
        <For each={props.panel.lines}>{(line) => <text>{line}</text>}</For>
      </box>
    </Show>
  );
}
