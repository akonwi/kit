import type {
  SessionEntry,
  SessionMessageEntry,
  ModelChangeEntry,
  ThinkingLevelChangeEntry,
  CompactionEntry,
  BranchSummaryEntry,
  CustomMessageEntry,
} from "@mariozechner/pi-coding-agent";
import type { TranscriptItem, TranscriptRole } from "../../state/app-state";

/**
 * Map a list of session branch entries into TranscriptItems for rendering.
 *
 * This is intentionally lossy for display purposes — it extracts
 * the human-readable content from each entry. The full session
 * entries remain available in the session manager for anything
 * that needs exact fidelity.
 */
export function mapBranchToTranscript(entries: SessionEntry[]): TranscriptItem[] {
  const items: TranscriptItem[] = [];

  for (const entry of entries) {
    const mapped = mapEntry(entry);
    if (mapped) {
      mapped.rawEntry = entry;
      items.push(mapped);
    }
  }

  return items;
}

function mapEntry(entry: SessionEntry): TranscriptItem | null {
  switch (entry.type) {
    case "message":
      return mapMessageEntry(entry);
    case "model_change":
      return mapModelChange(entry);
    case "thinking_level_change":
      return mapThinkingLevelChange(entry);
    case "compaction":
      return mapCompaction(entry);
    case "branch_summary":
      return mapBranchSummary(entry);
    case "custom_message":
      return mapCustomMessage(entry);
    case "custom":
    case "label":
    case "session_info":
      // These are metadata entries, not transcript-visible
      return null;
    default:
      return null;
  }
}

function mapMessageEntry(entry: SessionMessageEntry): TranscriptItem | null {
  const msg = entry.message;
  if (!msg) return null;

  switch (msg.role) {
    case "user":
      return mapUserMessage(entry);
    case "assistant":
      return mapAssistantMessage(entry);
    case "toolResult":
      return mapToolResult(entry);
    default:
      return null;
  }
}

function mapUserMessage(entry: SessionMessageEntry): TranscriptItem {
  const msg = entry.message as { role: "user"; content: string | Array<{ type: string; text?: string }> };
  let text: string;

  if (typeof msg.content === "string") {
    text = msg.content;
  } else if (Array.isArray(msg.content)) {
    text = msg.content
      .filter((c): c is { type: "text"; text: string } => c.type === "text" && typeof c.text === "string")
      .map((c) => c.text)
      .join("\n");
  } else {
    text = "";
  }

  return {
    id: entry.id,
    role: "user",
    lines: text.split("\n"),
  };
}

function mapAssistantMessage(entry: SessionMessageEntry): TranscriptItem {
  const msg = entry.message as {
    role: "assistant";
    content: Array<{ type: string; text?: string; thinking?: string; name?: string; arguments?: Record<string, unknown> }>;
  };

  const lines: string[] = [];

  for (const block of msg.content) {
    switch (block.type) {
      case "text":
        if (block.text) {
          lines.push(...block.text.split("\n"));
        }
        break;
      case "toolCall":
        lines.push(`$ ${block.name}${block.arguments ? formatToolArgs(block.arguments) : ""}`);
        break;
      // Thinking blocks are intentionally omitted from the transcript
      // to match the Amp-like feel where thinking is not shown inline
    }
  }

  // If assistant message has only tool calls and no text, still show the tool calls
  if (lines.length === 0) {
    const toolCalls = msg.content.filter((c) => c.type === "toolCall");
    for (const tc of toolCalls) {
      lines.push(`$ ${tc.name}`);
    }
  }

  return {
    id: entry.id,
    role: "assistant",
    lines: lines.length > 0 ? lines : ["(no visible content)"],
  };
}

function formatToolArgs(args: Record<string, unknown>): string {
  // Show a compact summary of tool arguments
  if ("command" in args && typeof args.command === "string") {
    return ` ${args.command}`;
  }
  if ("path" in args && typeof args.path === "string") {
    return ` ${args.path}`;
  }
  return "";
}

function mapToolResult(entry: SessionMessageEntry): TranscriptItem {
  const msg = entry.message as {
    role: "toolResult";
    toolName: string;
    content: Array<{ type: string; text?: string }>;
    isError: boolean;
  };

  const prefix = msg.isError ? "✗" : "✓";
  const lines: string[] = [];

  for (const block of msg.content) {
    if (block.type === "text" && block.text) {
      // Truncate long tool output for display
      const outputLines = block.text.split("\n");
      if (outputLines.length > 20) {
        lines.push(...outputLines.slice(0, 18));
        lines.push(`  ... (${outputLines.length - 18} more lines)`);
      } else {
        lines.push(...outputLines);
      }
    }
  }

  return {
    id: entry.id,
    role: "tool",
    lines: lines.length > 0 ? [`${prefix} ${msg.toolName}`, ...lines] : [`${prefix} ${msg.toolName}`],
  };
}

function mapModelChange(entry: ModelChangeEntry): TranscriptItem {
  return {
    id: entry.id,
    role: "meta",
    lines: [`model → ${entry.modelId || "(default)"}`],
  };
}

function mapThinkingLevelChange(entry: ThinkingLevelChangeEntry): TranscriptItem {
  return {
    id: entry.id,
    role: "meta",
    lines: [`thinking → ${entry.thinkingLevel}`],
  };
}

function mapCompaction(entry: CompactionEntry): TranscriptItem {
  return {
    id: entry.id,
    role: "meta",
    lines: [`── context compacted (${entry.tokensBefore} tokens before) ──`],
  };
}

function mapBranchSummary(entry: BranchSummaryEntry): TranscriptItem {
  const summaryLines = entry.summary.split("\n").slice(0, 5);
  return {
    id: entry.id,
    role: "meta",
    lines: ["── branch summary ──", ...summaryLines],
  };
}

function mapCustomMessage(entry: CustomMessageEntry): TranscriptItem | null {
  if (!entry.display) return null;

  let text: string;
  if (typeof entry.content === "string") {
    text = entry.content;
  } else if (Array.isArray(entry.content)) {
    text = entry.content
      .filter((c): c is { type: "text"; text: string } => c.type === "text" && typeof c.text === "string")
      .map((c) => c.text)
      .join("\n");
  } else {
    text = "";
  }

  return {
    id: entry.id,
    role: "system",
    lines: text.split("\n"),
  };
}
