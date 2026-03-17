import type { AgentMessage } from "@mariozechner/pi-agent-core";
import type {
  AssistantMessage,
  ToolCall,
  ToolResultMessage,
  UserMessage,
} from "@mariozechner/pi-ai";
import { TextAttributes } from "@opentui/core";
import { createSignal, For, onCleanup, Show } from "solid-js";
import { useRenderer } from "@opentui/solid";
import { syntaxStyle, theme } from "./theme";

const ABORTED_ATTRS = TextAttributes.DIM | TextAttributes.STRIKETHROUGH;

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

function MessageEntry(props: {
  msg: AgentMessage;
  toolResults: Map<string, ToolResultMessage>;
  aborted?: boolean;
}) {
  if (!("role" in props.msg)) return null;

  switch (props.msg.role) {
    case "user":
      return <UserEntry msg={props.msg as UserMessage} aborted={props.aborted} />;
    case "assistant":
      return (
        <AssistantEntry
          msg={props.msg as AssistantMessage}
          toolResults={props.toolResults}
          aborted={props.aborted}
        />
      );
    default:
      return null;
  }
}

/**
 * Build a set of message indices that belong to aborted turns.
 * An aborted turn is identified by an assistant message with
 * stopReason === "aborted", plus all messages from the preceding
 * user message onward.
 */
function buildAbortedSet(messages: AgentMessage[]): Set<number> {
  const aborted = new Set<number>();
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i];
    if (!("role" in msg) || msg.role !== "assistant") continue;
    const assistant = msg as AssistantMessage;
    if (assistant.stopReason !== "aborted") continue;

    // Mark the assistant message and everything back to its user message
    for (let j = i; j >= 0; j--) {
      aborted.add(j);
      const m = messages[j];
      if ("role" in m && m.role === "user") break;
    }
  }
  return aborted;
}

export function TranscriptPane(props: TranscriptPaneProps) {
  const toolResults = () => buildToolResultMap(props.messages);
  const abortedSet = () => buildAbortedSet(props.messages);

  // Pair each visible message with its original index so we can
  // look it up in the aborted set.
  const visibleEntries = () => {
    const entries: Array<{ msg: AgentMessage; idx: number }> = [];
    for (let i = 0; i < props.messages.length; i++) {
      const msg = props.messages[i];
      if (isStandaloneMessage(msg)) {
        entries.push({ msg, idx: i });
      }
    }
    return entries;
  };

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
        <For each={visibleEntries()}>
          {(entry) => (
            <MessageEntry
              msg={entry.msg}
              toolResults={toolResults()}
              aborted={abortedSet().has(entry.idx)}
            />
          )}
        </For>
      </box>
    </scrollbox>
  );
}
