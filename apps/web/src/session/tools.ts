// How a tool call reads in the transcript: a title, a chip naming what it
// worked on, and short facts. Follows the Apple clients' ToolPresentation.
import type { ToolCall } from "./projection";

export type FactTone = "muted" | "success" | "danger";

export interface Fact {
  text: string;
  tone: FactTone;
}

/** A plugin tool's name leads with its plugin: `git__status`. */
const PLUGIN_PREFIX = /^[a-z][a-z0-9-]{0,31}__/;

/** The icon a tool row shows, chosen as the Mac app's ToolActivityView does. */
export type ToolIcon =
  | "terminal"
  | "file"
  | "filePlus"
  | "files"
  | "edit"
  | "search"
  | "folder"
  | "ask"
  | "agent"
  | "image"
  | "skill"
  | "note"
  | "notePencil"
  | "plugin"
  | "peer"
  | "newSession"
  | "tool";

export interface ToolPresentation {
  title: string;
  chip?: string;
  /** Whether the chip leads with a path, whose end is kept when it's cut. */
  path: boolean;
  /** The file the call worked on, and the line it started at, which the chip opens. */
  file?: { path: string; line?: number };
  /** The subagent the call worked with, whose conversation the chip opens. */
  agent?: string;
  facts: Fact[];
  icon: ToolIcon;
  /** Whether the row shows how long the call ran. */
  timed: boolean;
}

const INTERACTION_TOOLS = new Set([
  "confirm_from_user",
  "input_from_user",
  "select_from_user",
  "guided_questions",
]);

/** A subagent call's title by its action, as the Apple clients name them. */
const SUBAGENT_TITLES: Record<string, string> = {
  start: "Start agent",
  run: "Start agent",
  spawn: "Start agent",
  message: "Message agent",
  send: "Message agent",
  ask: "Ask agent",
  wait: "Wait for agent",
  cancel: "Cancel agent",
  dismiss: "Dismiss agent",
  inspect: "Inspect agent",
};

function parse(text: string | undefined): Record<string, unknown> {
  if (!text) return {};
  try {
    const value = JSON.parse(text) as unknown;
    return value && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

function oneLine(value: string): string {
  return value.split(/\s+/).filter(Boolean).join(" ");
}

/** A tool name in sentence case: `fetch_url` reads "Fetch url". */
export function humanize(name: string): string {
  const words = name
    .replace(/[_-]/g, " ")
    .split(" ")
    .filter(Boolean)
    .map((word) => word.toLowerCase());
  const first = words[0];
  if (!first) return "Tool call";
  return [first[0]?.toUpperCase() + first.slice(1), ...words.slice(1)].join(" ");
}

function lines(text: string): string[] {
  if (!text) return [];
  const result = text.split("\n");
  if (result.at(-1) === "") result.pop();
  return result;
}

/** Changed lines of one edit, counted as a line diff would. */
function changedLines(oldText: string, newText: string): { added: number; removed: number } {
  const before = lines(oldText);
  const after = lines(newText);
  let start = 0;
  while (start < before.length && start < after.length && before[start] === after[start]) start += 1;
  let end = 0;
  while (
    end < before.length - start &&
    end < after.length - start &&
    before[before.length - 1 - end] === after[after.length - 1 - end]
  ) {
    end += 1;
  }
  return { removed: before.length - start - end, added: after.length - start - end };
}

export function presentTool(tool: ToolCall): ToolPresentation {
  const args = parse(tool.arguments);
  const details = tool.details ?? {};
  const kind = tool.name.toLowerCase();
  const text = (key: string): string | undefined => {
    const value = args[key];
    return typeof value === "string" && value.trim() ? value.trim() : undefined;
  };
  const count = (key: string): number | undefined => {
    const value = details[key];
    return typeof value === "number" ? value : undefined;
  };
  const fallback = (): string | undefined => {
    for (const key of ["command", "path", "agent", "url", "query", "name"]) {
      const value = text(key);
      if (value) return oneLine(value);
    }
    const raw = oneLine(tool.arguments ?? "");
    return raw && raw !== "{}" ? raw : undefined;
  };
  const done = tool.status === "done" && !tool.failed;
  const facts: Fact[] = [];
  const presentation = (
    title: string,
    chip: string | undefined,
    icon: ToolIcon,
    options: {
      path?: boolean;
      timed?: boolean;
      file?: ToolPresentation["file"];
      agent?: string;
    } = {},
  ): ToolPresentation => ({
    title,
    chip,
    icon,
    facts,
    path: options.path ?? false,
    timed: options.timed ?? false,
    ...(options.file ? { file: options.file } : {}),
    ...(options.agent ? { agent: options.agent } : {}),
  });

  if (kind === "bash") {
    const exit = count("exitCode");
    if (tool.status === "done") {
      if (details.timedOut === true) facts.push({ text: "timed out", tone: "danger" });
      else if (exit !== undefined && exit !== 0) facts.push({ text: `exit ${exit}`, tone: "danger" });
    }
    return presentation("Run command", text("command") && oneLine(text("command") as string), "terminal", {
      timed: true,
    });
  }

  switch (kind) {
    case "read": {
      const path = text("path");
      const start = Math.max(1, typeof args.offset === "number" ? Math.floor(args.offset) : 1);
      let chip = path;
      if (done && path) {
        const truncated = /\n\[truncated\]\n?$/.test(tool.output);
        const shown = lines(truncated ? tool.output.replace(/\n\[truncated\]\n?$/, "") : tool.output).length;
        chip =
          path +
          (shown > 0 ? `:${start}–${start + shown - 1}` : " · empty") +
          (truncated ? " · truncated" : "");
      }
      return presentation("Read file", chip, "file", {
        path: true,
        file: path ? { path, ...(start > 1 ? { line: start } : {}) } : undefined,
      });
    }
    case "write": {
      const content = typeof args.content === "string" ? args.content : undefined;
      const count = content === undefined ? undefined : lines(content).length;
      const title = count === undefined ? "Write file" : `Write ${count === 1 ? "1 line" : `${count} lines`}`;
      const path = text("path");
      return presentation(title, path ?? fallback(), "filePlus", {
        path: true,
        file: path ? { path } : undefined,
      });
    }
    case "edit":
    case "edit_scratchpad": {
      const edits = (Array.isArray(args.edits) ? args.edits : [args]).filter(
        (edit): edit is { oldText: string; newText: string } =>
          typeof edit === "object" &&
          edit !== null &&
          typeof (edit as Record<string, unknown>).oldText === "string" &&
          typeof (edit as Record<string, unknown>).newText === "string",
      );
      if (done) {
        let added = 0;
        let removed = 0;
        for (const edit of edits) {
          const changed = changedLines(edit.oldText, edit.newText);
          added += changed.added;
          removed += changed.removed;
        }
        if (added + removed > 0) {
          facts.push({ text: `+${added}`, tone: "success" }, { text: `−${removed}`, tone: "danger" });
        }
      }
      if (kind === "edit_scratchpad") {
        return presentation(
          "Update scratchpad",
          edits.length === 1 ? "1 edit" : `${edits.length} edits`,
          "notePencil",
        );
      }
      const title =
        edits.length === 0
          ? "Edit file"
          : edits.length === 1
            ? "Edit 1 section"
            : `Edit ${edits.length} sections`;
      const path = text("path");
      return presentation(title, path ?? fallback(), "edit", {
        path: true,
        file: path ? { path } : undefined,
      });
    }
    case "grep": {
      const matches = count("matchCount");
      if (done && matches !== undefined)
        facts.push({ text: matches === 1 ? "1 match" : `${matches} matches`, tone: "muted" });
      const scope = text("path") ?? ".";
      const pattern = text("pattern");
      return presentation("Search", pattern ? `${pattern} in ${scope}` : scope, "search");
    }
    case "find":
    case "glob": {
      const files = count("count");
      if (done && files !== undefined)
        facts.push({ text: files === 1 ? "1 file" : `${files} files`, tone: "muted" });
      const scope = text("path") ?? ".";
      const pattern = text("pattern");
      return presentation("Find files", pattern ? `${pattern} in ${scope}` : scope, "files");
    }
    case "ls": {
      const entries = count("count");
      if (done && entries !== undefined) {
        facts.push({ text: entries === 1 ? "1 entry" : `${entries} entries`, tone: "muted" });
      }
      return presentation("List directory", text("path") ?? fallback(), "folder", { path: true });
    }
    case "change_cwd":
      return presentation("Change directory", text("path") ?? fallback(), "folder", { path: true });
    case "read_scratchpad":
      return presentation("Read scratchpad", undefined, "note");
    case "activate_skill":
      return presentation("Load skill", text("name"), "skill");
    case "subagent": {
      const agent = text("agent");
      const action = text("action")?.toLowerCase();
      const link = tool.status === "notRun" ? undefined : agent;
      if (action === "inspect_request") return presentation("Inspect request", text("receipt"), "agent");
      if (action === "list_agents") return presentation("List agents", undefined, "agent");
      const title = action ? SUBAGENT_TITLES[action] : undefined;
      // A start or a message returns at once; waiting and asking take as long as the agent.
      const timed = action === "wait" || action === "ask";
      return presentation(title ?? "Agent", agent, "agent", { timed, agent: link });
    }
    case "subagent_inbox":
      return presentation("Check inbox", undefined, "agent");
    case "subagent_send":
      return presentation("Message sibling", text("agent"), "agent");
    case "subagent_reply":
      return presentation("Reply", text("receipt"), "agent");
    case "subagent_inspect":
      return presentation("Inspect request", text("receipt"), "agent");
    case "show_image":
    case "inspect_image":
      return presentation(kind === "show_image" ? "Show image" : "Inspect image", text("path"), "image", {
        path: true,
      });
    case "open_url":
      return presentation("Open URL", text("url"), "tool");
    default:
      if (INTERACTION_TOOLS.has(kind)) {
        const title =
          kind === "confirm_from_user" ? "Confirm" : kind === "guided_questions" ? "Ask questions" : "Ask";
        return presentation(title, text("title") && oneLine(text("title") as string), "ask");
      }
      if (kind === "peer_session") return presentation("Peer session", fallback(), "peer", { timed: true });
      if (kind === "create_session") return presentation("New session", fallback(), "newSession");
      return presentation(
        humanize(tool.name.replace(PLUGIN_PREFIX, "")),
        fallback(),
        PLUGIN_PREFIX.test(tool.name) ? "plugin" : "tool",
        { timed: true },
      );
  }
}

/** A duration as the tool row shows it: 0:14, 2:05, 1:02:03. */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = String(total % 60).padStart(2, "0");
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, "0")}:${seconds}` : `${minutes}:${seconds}`;
}
