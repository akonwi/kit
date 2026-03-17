import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type {
  AssistantMessage,
  ToolResultMessage,
  UserMessage,
} from "@mariozechner/pi-ai";
import { For, Show } from "solid-js";
import { syntaxStyle, theme } from "./theme";

export type TranscriptPaneProps = {
  messages: AgentMessage[];
};

// ── Helpers ──────────────────────────────────────────────────────────

function extractUserText(msg: UserMessage): string {
  if (typeof msg.content === "string") return msg.content;
  if (Array.isArray(msg.content)) {
    return msg.content
      .filter(
        (c): c is { type: "text"; text: string } =>
          c.type === "text" && typeof c.text === "string",
      )
      .map((c) => c.text)
      .join("\n");
  }
  return "";
}

function extractAssistantText(msg: AssistantMessage): string {
  const parts: string[] = [];
  for (const block of msg.content) {
    if (block.type === "text" && "text" in block && block.text) {
      parts.push(block.text);
    } else if (block.type === "toolCall" && "name" in block) {
      const tc = block as {
        type: "toolCall";
        name: string;
        arguments?: Record<string, unknown>;
      };
      parts.push(`\`$ ${tc.name}${formatToolArgs(tc.arguments)}\``);
    }
  }
  if (parts.length === 0) {
    for (const block of msg.content) {
      if (block.type === "toolCall" && "name" in block) {
        parts.push(`\`$ ${(block as { name: string }).name}\``);
      }
    }
  }
  return parts.join("\n\n");
}

function extractToolResultLines(msg: ToolResultMessage): string[] {
  const lines: string[] = [];
  for (const block of msg.content) {
    if (block.type === "text" && "text" in block && block.text) {
      const outputLines = block.text.split("\n");
      if (outputLines.length > 20) {
        lines.push(...outputLines.slice(0, 18));
        lines.push(`  ... (${outputLines.length - 18} more lines)`);
      } else {
        lines.push(...outputLines);
      }
    }
  }
  return lines;
}

function formatToolArgs(args?: Record<string, unknown>): string {
  if (!args) return "";
  if ("command" in args && typeof args.command === "string")
    return ` ${args.command}`;
  if ("path" in args && typeof args.path === "string") return ` ${args.path}`;
  return "";
}

function isAssistantError(msg: AssistantMessage): boolean {
  return msg.stopReason === "error" && !!msg.errorMessage;
}

// ── Entry renderers ──────────────────────────────────────────────────

function UserEntry(props: { msg: UserMessage; onClick?: () => void }) {
  const text = extractUserText(props.msg);
  return (
    <box
      border={["left"] as any}
      borderColor={theme.userBorder}
      paddingLeft={1}
      flexDirection="column"
      gap={0}
      width="100%"
      onMouseDown={props.onClick}
    >
      <code
        filetype="markdown"
        content={text}
        syntaxStyle={syntaxStyle}
        conceal
        drawUnstyledText={false}
        fg={theme.textPrimary}
      />
    </box>
  );
}

function AssistantEntry(props: {
  msg: AssistantMessage;
  onClick?: () => void;
}) {
  if (isAssistantError(props.msg)) {
    return (
      <box
        paddingLeft={1}
        flexDirection="column"
        gap={0}
        width="100%"
        onMouseDown={props.onClick}
      >
        <text fg={theme.errorText}>{props.msg.errorMessage}</text>
      </box>
    );
  }

  const text = extractAssistantText(props.msg);
  return (
    <Show when={text.length > 0}>
      <box
        flexDirection="column"
        gap={0}
        width="100%"
        onMouseDown={props.onClick}
      >
        <code
          filetype="markdown"
          content={text}
          syntaxStyle={syntaxStyle}
          conceal
          drawUnstyledText={false}
          fg={theme.textPrimary}
        />
      </box>
    </Show>
  );
}

function ToolResultEntry(props: {
  msg: ToolResultMessage;
  onClick?: () => void;
}) {
  const prefix = props.msg.isError ? "✗" : "✓";
  const headerColor = props.msg.isError ? theme.errorText : theme.toolText;
  const lines = extractToolResultLines(props.msg);
  return (
    <box
      flexDirection="column"
      gap={0}
      width="100%"
      onMouseDown={props.onClick}
    >
      <text fg={headerColor}>
        {prefix} {props.msg.toolName}
      </text>
      <For each={lines}>
        {(line) => <text fg={theme.toolText}>{line}</text>}
      </For>
    </box>
  );
}

// ── Main component ───────────────────────────────────────────────────

function MessageEntry(props: { msg: AgentMessage; onClick?: () => void }) {
  if (!("role" in props.msg)) return null;

  switch (props.msg.role) {
    case "user":
      return (
        <UserEntry msg={props.msg as UserMessage} onClick={props.onClick} />
      );
    case "assistant":
      return (
        <AssistantEntry
          msg={props.msg as AssistantMessage}
          onClick={props.onClick}
        />
      );
    case "toolResult":
      return (
        <ToolResultEntry
          msg={props.msg as ToolResultMessage}
          onClick={props.onClick}
        />
      );
    default:
      return null;
  }
}

export function TranscriptPane(props: TranscriptPaneProps) {
  return (
    <scrollbox
      flexGrow={1}
      height="100%"
      scrollY
      stickyStart="bottom"
      stickyScroll
      padding={1}
      style={{
        scrollbarOptions: {
          trackOptions: {
            foregroundColor: theme.scrollbarFg,
            backgroundColor: theme.scrollbarBg,
          },
        },
      }}
    >
      <box flexDirection="column" gap={1} width="100%">
        <Show when={props.messages.length === 0}>
          <box flexDirection="column" gap={0} width="100%">
            <text fg={theme.textSecondary}>pi-kit</text>
            <text fg={theme.textSecondary}>Start a conversation below.</text>
          </box>
        </Show>
        <For each={props.messages}>
          {(msg) => <MessageEntry msg={msg} onClick={() => console.log(msg)} />}
        </For>
      </box>
    </scrollbox>
  );
}
