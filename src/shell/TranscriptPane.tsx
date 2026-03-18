import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type {
  AssistantMessage,
  ToolCall,
  ToolResultMessage,
  UserMessage,
} from "@mariozechner/pi-ai";

// BashExecutionMessage type from pi-coding-agent
interface BashExecutionMessage {
  role: "bashExecution";
  command: string;
  output: string;
  exitCode: number | undefined;
  cancelled: boolean;
  truncated: boolean;
  fullOutputPath?: string;
  timestamp: number;
}
import { TextAttributes } from "@opentui/core";
import { createSignal, For, onCleanup, Show } from "solid-js";
import { useRenderer } from "@opentui/solid";
import { syntaxStyle, theme } from "./theme";

const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;

import type { AppNotice } from "../state/app-state";

export type TranscriptPaneProps = {
  messages: AgentMessage[];
  notices?: AppNotice[];
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

function extractAssistantParts(msg: AssistantMessage): {
  text: string;
  toolCalls: ToolCall[];
} {
  const textParts: string[] = [];
  const toolCalls: ToolCall[] = [];
  for (const block of msg.content) {
    if (block.type === "text" && "text" in block && block.text) {
      textParts.push(block.text);
    } else if (block.type === "toolCall" && "name" in block) {
      toolCalls.push(block as ToolCall);
    }
    // thinking blocks are omitted
  }
  return { text: textParts.join("\n\n"), toolCalls };
}

function extractToolResultLines(msg: ToolResultMessage): string[] {
  const lines: string[] = [];
  for (const block of msg.content) {
    if (block.type === "text" && "text" in block && block.text) {
      lines.push(...block.text.split("\n"));
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

function isBashExecution(msg: AgentMessage): msg is BashExecutionMessage {
  return "role" in msg && msg.role === "bashExecution";
}

// ── Spinner ──────────────────────────────────────────────────────────

const SPINNER_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

function InlineSpinner() {
  const [frame, setFrame] = createSignal(0);
  const timer = setInterval(() => {
    setFrame((f) => (f + 1) % SPINNER_FRAMES.length);
  }, 80);
  onCleanup(() => clearInterval(timer));
  return <text fg={theme.toolText}>{SPINNER_FRAMES[frame()]}</text>;
}

// ── Entry renderers ──────────────────────────────────────────────────

function UserEntry(props: { msg: UserMessage; aborted?: boolean }) {
  const text = extractUserText(props.msg);
  return (
    <box
      border={["left"] as any}
      borderColor={props.aborted ? theme.textMuted : theme.userBorder}
      paddingLeft={1}
      flexDirection="column"
      gap={0}
      width="100%"
    >
      <code
        filetype="markdown"
        content={text}
        syntaxStyle={syntaxStyle}
        conceal
        drawUnstyledText={false}
        fg={props.aborted ? theme.textMuted : theme.textPrimary}
        attributes={props.aborted ? ABORTED_ATTRS : undefined}
      />
    </box>
  );
}

function BashEntry(props: { msg: BashExecutionMessage }) {
  const [expanded, setExpanded] = createSignal(true); // expanded by default for user-invoked commands

  const outputLines = () => props.msg.output.split("\n");
  const hasOutput = outputLines().length > 0;
  const prefix = props.msg.cancelled ? "⊘" : props.msg.exitCode === 0 ? "✓" : "✗";
  const prefixColor = props.msg.cancelled
    ? theme.textMuted
    : props.msg.exitCode === 0
      ? theme.toolText
      : theme.errorText;

  const displayLines = () => {
    if (!expanded()) return [];
    if (outputLines().length > 20) {
      return [...outputLines().slice(0, 18), `  ... (${outputLines().length - 18} more lines)`];
    }
    return outputLines();
  };

  return (
    <box
      border={["left"] as any}
      borderColor={theme.toolText}
      paddingLeft={1}
      flexDirection="column"
      gap={0}
      width="100%"
    >
      <box
        flexDirection="row"
        gap={1}
        onMouseDown={() => hasOutput && setExpanded(!expanded())}
      >
        <text fg={prefixColor}>{prefix}</text>
        <code
          filetype="bash"
          content={props.msg.command}
          syntaxStyle={syntaxStyle}
          fg={theme.textPrimary}
        />
        <Show when={hasOutput}>
          <text fg={theme.textMuted}>
            {expanded() ? "▾" : "▸"} {outputLines().length} line{outputLines().length === 1 ? "" : "s"}
          </text>
        </Show>
      </box>
      <Show when={expanded()}>
        <box paddingLeft={2} flexDirection="column" gap={0}>
          <For each={displayLines()}>
            {(line) => <text fg={theme.textMuted}>{line}</text>}
          </For>
        </box>
      </Show>
    </box>
  );
}

/**
 * A pending tool call — no result yet. Shows spinner + tool name.
 */
function PendingToolCall(props: { tc: ToolCall; aborted?: boolean }) {
  return (
    <box flexDirection="row" gap={1}>
      <Show when={!props.aborted} fallback={<text fg={theme.textMuted}>⊘</text>}>
        <InlineSpinner />
      </Show>
      <text fg={props.aborted ? theme.textMuted : theme.toolText} attributes={props.aborted ? ABORTED_ATTRS : undefined}>
        {props.tc.name}{formatToolArgs(props.tc.arguments)}
      </text>
    </box>
  );
}

/**
 * A completed tool call — shows result header (✓/✗) with collapsible output.
 */
function CompletedToolCall(props: { tc: ToolCall; result: ToolResultMessage; aborted?: boolean }) {
  const [expanded, setExpanded] = createSignal(false);
  const renderer = useRenderer();
  const lines = extractToolResultLines(props.result);
  const prefix = props.aborted ? "⊘" : props.result.isError ? "✗" : "✓";
  const headerColor = props.aborted ? theme.textMuted : props.result.isError ? theme.errorText : theme.toolText;
  const hasOutput = lines.length > 0;

  const displayLines = () => {
    if (!expanded()) return [];
    if (lines.length > 40) {
      return [...lines.slice(0, 38), `  ... (${lines.length - 38} more lines)`];
    }
    return lines;
  };

  return (
    <box flexDirection="column" gap={0} width="100%">
      <box
        flexDirection="row"
        gap={1}
        onMouseDown={() => {
          if (renderer.getSelection()?.getSelectedText()) return;
          if (hasOutput) setExpanded(!expanded());
        }}
      >
        <text fg={headerColor} attributes={props.aborted ? ABORTED_ATTRS : undefined}>
          {prefix} {props.tc.name}{formatToolArgs(props.tc.arguments)}
        </text>
        <Show when={hasOutput && !props.aborted}>
          <text fg={theme.textMuted}>
            {expanded() ? "▾" : "▸"} {lines.length} line{lines.length === 1 ? "" : "s"}
          </text>
        </Show>
      </box>
      <Show when={expanded()}>
        <box paddingLeft={2} flexDirection="column" gap={0}>
          <For each={displayLines()}>
            {(line) => <text fg={theme.textMuted}>{line}</text>}
          </For>
        </box>
      </Show>
    </box>
  );
}

function AssistantEntry(props: {
  msg: AssistantMessage;
  toolResults: Map<string, ToolResultMessage>;
  aborted?: boolean;
}) {
  if (isAssistantError(props.msg)) {
    return (
      <box paddingLeft={1} flexDirection="column" gap={0} width="100%">
        <text fg={theme.errorText}>{props.msg.errorMessage}</text>
      </box>
    );
  }

  const { text, toolCalls } = extractAssistantParts(props.msg);

  return (
    <box flexDirection="column" gap={0} width="100%">
      {/* Tool calls — merged with their results */}
      <For each={toolCalls}>
        {(tc) => {
          const result = () => props.toolResults.get(tc.id);
          return (
            <Show when={result()} fallback={<PendingToolCall tc={tc} aborted={props.aborted} />}>
              <CompletedToolCall tc={tc} result={result()!} aborted={props.aborted} />
            </Show>
          );
        }}
      </For>

      {/* Text content */}
      <Show when={text.length > 0}>
        <code
          filetype="markdown"
          content={text}
          syntaxStyle={syntaxStyle}
          conceal
          drawUnstyledText={false}
          fg={props.aborted ? theme.textMuted : theme.textPrimary}
          attributes={props.aborted ? ABORTED_ATTRS : undefined}
        />
      </Show>
    </box>
  );
}

// ── Main component ───────────────────────────────────────────────────

/**
 * Build a map from toolCallId → ToolResultMessage for pairing
 * tool calls with their results.
 */
function buildToolResultMap(messages: AgentMessage[]): Map<string, ToolResultMessage> {
  const map = new Map<string, ToolResultMessage>();
  for (const msg of messages) {
    if ("role" in msg && msg.role === "toolResult") {
      const tr = msg as ToolResultMessage;
      map.set(tr.toolCallId, tr);
    }
  }
  return map;
}

/**
 * Filter out standalone toolResult messages — they're rendered
 * inline within their parent AssistantEntry.
 */
function isStandaloneMessage(msg: AgentMessage): boolean {
  if (!("role" in msg)) return false;
  return msg.role !== "toolResult";
}

function NoticeEntry(props: { notice: AppNotice }) {
  const color = () => props.notice.variant === "error" ? theme.errorText : theme.textMuted;
  return (
    <box
      border={["left"] as any}
      borderColor={color()}
      paddingLeft={1}
      flexDirection="column"
      gap={0}
      width="100%"
    >
      <text fg={color()}>{props.notice.title}: {props.notice.lines.join("\n")}</text>
    </box>
  );
}

function MessageEntry(props: {
  msg: AgentMessage;
  toolResults: Map<string, ToolResultMessage>;
}) {
  if (!("role" in props.msg)) return null;

  switch (props.msg.role) {
    case "user":
      return <UserEntry msg={props.msg as UserMessage} />;
    case "assistant":
      const assistant = props.msg as AssistantMessage;
      const aborted = assistant.stopReason === "aborted";
      return (
        <AssistantEntry
          msg={assistant}
          toolResults={props.toolResults}
          aborted={aborted}
        />
      );
    case "bashExecution":
      return <BashEntry msg={props.msg as BashExecutionMessage} />;
    default:
      return null;
  }
}

export function TranscriptPane(props: TranscriptPaneProps) {
  const toolResults = () => buildToolResultMap(props.messages);
  const visibleMessages = () => props.messages.filter(isStandaloneMessage);

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
        <For each={visibleMessages()}>
          {(msg) => <MessageEntry msg={msg} toolResults={toolResults()} />}
        </For>
        <For each={props.notices ?? []}>
          {(notice) => <NoticeEntry notice={notice} />}
        </For>
      </box>
    </scrollbox>
  );
}
