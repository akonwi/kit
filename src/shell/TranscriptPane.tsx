import { For } from "solid-js";
import type { TranscriptItem, TranscriptRole } from "../state/app-state";

export type TranscriptPaneProps = {
  items: TranscriptItem[];
};

function roleColor(role: TranscriptRole): string {
  switch (role) {
    case "user":
      return "#6cb6ff";
    case "assistant":
      return "#f2f2f2";
    case "tool":
      return "#7ee787";
    case "meta":
      return "#8f8f8f";
    case "system":
    default:
      return "#b8b8b8";
  }
}

function roleLabel(role: TranscriptRole): string {
  switch (role) {
    case "user":
      return "user";
    case "assistant":
      return "assistant";
    case "tool":
      return "tool";
    case "meta":
      return "meta";
    case "system":
    default:
      return "system";
  }
}

export function TranscriptPane(props: TranscriptPaneProps) {
  return (
    <scrollbox
      flexGrow={1}
      scrollY
      stickyScroll
      stickyStart="bottom"
      viewportCulling
      padding={1}
      style={{
        scrollbarOptions: {
          showArrows: true,
          trackOptions: {
            foregroundColor: "#6e6e6e",
            backgroundColor: "#2a2a2a",
          },
        },
      }}
    >
      <box flexDirection="column" gap={1} width="100%">
        <For each={props.items}>
          {(item) => (
            <box flexDirection="column" gap={0} width="100%">
              <text fg={roleColor(item.role)}>{roleLabel(item.role)}</text>
              <For each={item.lines}>{(line) => <text>{line}</text>}</For>
            </box>
          )}
        </For>
      </box>
    </scrollbox>
  );
}
