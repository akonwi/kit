import { For } from "solid-js";
import type { TranscriptItem, TranscriptRole } from "../state/app-state";

export type TranscriptPaneProps = {
  items: TranscriptItem[];
};

function textColor(role: TranscriptRole): string {
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

function TranscriptEntry(props: { item: TranscriptItem }) {
  const color = textColor(props.item.role);

  if (props.item.role === "user") {
    return (
      <box
        border={["left"] as any}
        borderColor="#6cb6ff"
        paddingLeft={1}
        flexDirection="column"
        gap={0}
        width="100%"
      >
        <For each={props.item.lines}>{(line) => <text fg={color}>{line}</text>}</For>
      </box>
    );
  }

  return (
    <box flexDirection="column" gap={0} width="100%">
      <For each={props.item.lines}>{(line) => <text fg={color}>{line}</text>}</For>
    </box>
  );
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
          {(item) => <TranscriptEntry item={item} />}
        </For>
      </box>
    </scrollbox>
  );
}
