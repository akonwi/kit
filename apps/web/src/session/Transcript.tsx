// The transcript: messages, grouped tool calls, and markers.
import { createMemo, createSignal, For, Match, onCleanup, Show, Switch } from "solid-js";
import type { SubmittedAnnotation } from "../api/types";
import { sentLabel } from "../changes/comments";
import { CodeBlock } from "../markdown/Fence";
import { Markdown } from "../markdown/Markdown";
import { copyText } from "../ui/copy";
import { Icon, Spinner } from "../ui/icons";
import { ReplyAttachments, SentAttachments } from "./AttachmentViews";
import type { Row, ToolCall } from "./projection";
import { type ShellRun, shellStatus } from "./shell";
import { formatDuration, presentTool } from "./tools";

/** A clock that ticks every second while anything reads it. */
const [clock, setClock] = createSignal(Date.now());
let readers = 0;
let ticking: ReturnType<typeof setInterval> | undefined;
export function useClock(): () => number {
  readers += 1;
  if (!ticking) ticking = setInterval(() => setClock(Date.now()), 1000);
  onCleanup(() => {
    readers -= 1;
    if (readers === 0 && ticking) {
      clearInterval(ticking);
      ticking = undefined;
    }
  });
  return clock;
}

export function TranscriptRow(props: { row: Row; last: boolean }) {
  const row = props.row;
  switch (row.kind) {
    case "user":
      return (
        <div class="message user">
          <Show when={row.text}>
            <p class="user-text">{row.text}</p>
          </Show>
          <SentAttachments items={row.attachments} />
          <Show when={row.comments.length > 0}>
            <ul class="sent-comments" aria-label="Comments">
              <For each={row.comments}>{(comment) => <SentComment comment={comment} />}</For>
            </ul>
          </Show>
        </div>
      );
    case "assistant":
      return (
        <div class="message assistant">
          <Show when={row.text}>
            <Markdown text={row.text} />
          </Show>
          <ReplyAttachments items={row.attachments} />
        </div>
      );
    case "tools":
      return (
        <>
          <ToolGroup tools={row.tools} live={props.last} />
          {/* What show_image shows stays in view, as Kit's own images do. */}
          <ReplyAttachments items={shownImages(row.tools)} />
        </>
      );
    case "plugin":
      return (
        <div class="marker plugin">
          <span class="marker-label">{row.plugin}</span>
          <Markdown text={row.text} />
        </div>
      );
    case "compaction":
      return (
        <details class="marker compaction">
          <summary>Earlier messages were summarized</summary>
          <Markdown text={row.summary} />
        </details>
      );
    case "cwd":
      return (
        <div class="marker cwd">
          <Icon name="folder" size={16} />
          <span>
            Moved to <code>{row.path}</code>
          </span>
        </div>
      );
    case "shell":
      return <ShellRow run={row.run} />;
    case "turnEnd":
      return (
        <div class="marker turn-end" data-end={row.end}>
          <Icon name={row.end === "failed" ? "alert" : "prohibit"} size={16} />
          <span>
            <strong>
              {row.end === "stopped" ? "Stopped" : row.end === "interrupted" ? "Interrupted" : "Failed"}
            </strong>{" "}
            {row.message}
          </span>
        </div>
      );
  }
}

/** A comment that went with a message: where, what, and the lines as they were sent. */
function SentComment(props: { comment: SubmittedAnnotation }) {
  return (
    <li class="sent-comment">
      <span
        class="sent-comment-where mono"
        title={props.comment.anchor.workingTreeDiff?.path ?? props.comment.anchor.workspaceFile?.path}
      >
        <Icon name="comment" size={16} />
        {sentLabel(props.comment)}
      </span>
      <p class="sent-comment-body">{props.comment.body}</p>
      <Show when={props.comment.preview.text}>
        <pre class="sent-comment-source">
          <code>{props.comment.preview.text.replace(/\n$/, "")}</code>
        </pre>
      </Show>
      <Show when={props.comment.preview.truncated}>
        <span class="muted small">The lines sent were cut short.</span>
      </Show>
    </li>
  );
}

/**
 * A shell command as the Mac shows it: the command, its outcome when it
 * didn't simply succeed, and its output below a rule, capped in height. An
 * accent edge means the agent saw it; one kept from it says so, since a
 * phone has no hover to explain a plain edge.
 */
function ShellRow(props: { run: ShellRun }) {
  const [copied, setCopied] = createSignal(false);
  const run = () => props.run;
  const label = () =>
    [shellStatus(run()), run().excluded ? "Not in context" : undefined].filter(Boolean).join(" · ");
  let reset: ReturnType<typeof setTimeout> | undefined;
  onCleanup(() => clearTimeout(reset));
  const copy = async () => {
    if (!(await copyText(run().command))) return;
    setCopied(true);
    clearTimeout(reset);
    reset = setTimeout(() => setCopied(false), 1500);
  };
  return (
    <section class="shell-row" data-excluded={run().excluded || undefined} aria-label="Shell command">
      <div class="shell-head">
        <Show
          when={run().status === "running"}
          fallback={<Icon name="terminal" size={14} class="shell-icon" />}
        >
          <Spinner size={14} class="shell-icon" />
        </Show>
        <code class="shell-command">{run().command}</code>
        <Show when={label()}>
          <span class="shell-status">{label()}</span>
        </Show>
        <button type="button" class="icon-button small shell-copy" aria-label="Copy command" onClick={copy}>
          <Icon name={copied() ? "check" : "copy"} size={14} />
        </button>
      </div>
      <Show when={run().output}>
        <pre class="shell-output">{run().output}</pre>
      </Show>
      <Show when={run().error}>
        <p class="shell-error">{run().error}</p>
      </Show>
      <Show when={run().truncated}>
        <p class="shell-note">Output truncated</p>
      </Show>
    </section>
  );
}

/** The images show_image calls showed. */
function shownImages(tools: ToolCall[]) {
  return tools.flatMap((tool) =>
    tool.name === "show_image" ? tool.attachments.filter((item) => item.image) : [],
  );
}

/** A result's other files, in its detail. */
function resultFiles(tool: ToolCall) {
  return tool.name === "show_image" ? tool.attachments.filter((item) => !item.image) : tool.attachments;
}

function ToolGroup(props: { tools: ToolCall[]; live: boolean }) {
  const active = () => props.tools.some((tool) => tool.status === "running" || tool.status === "planned");
  const [open, setOpen] = createSignal<boolean | undefined>();
  const expanded = () => open() ?? (props.live || active());
  return (
    <Show
      when={props.tools.length > 1}
      fallback={<For each={props.tools}>{(tool) => <ToolRow tool={tool} />}</For>}
    >
      <div class="tool-group">
        <button
          type="button"
          class="tool-group-toggle"
          aria-expanded={expanded()}
          onClick={() => setOpen(!expanded())}
        >
          <Icon name={expanded() ? "chevDown" : "chevRight"} size={16} />
          <span>{props.tools.length} tool calls</span>
        </button>
        <Show when={expanded()}>
          <For each={props.tools}>{(tool) => <ToolRow tool={tool} />}</For>
        </Show>
      </div>
    </Show>
  );
}

function ToolRow(props: { tool: ToolCall }) {
  const presentation = createMemo(() => presentTool(props.tool));
  const [open, setOpen] = createSignal(false);
  const expandable = () =>
    props.tool.status !== "notRun" &&
    Boolean(props.tool.output || props.tool.arguments || resultFiles(props.tool).length > 0);
  return (
    <div class="tool" data-status={props.tool.status} data-failed={props.tool.failed || undefined}>
      <button
        type="button"
        class="tool-row"
        aria-expanded={expandable() ? open() : undefined}
        disabled={!expandable()}
        onClick={() => setOpen(!open())}
      >
        <span class="tool-icon">
          <Switch fallback={<Icon name={presentation().icon} size={16} />}>
            <Match when={props.tool.status === "running"}>
              <Spinner size={14} />
            </Match>
            <Match when={props.tool.status === "notRun"}>
              <Icon name="prohibit" size={16} />
            </Match>
            <Match when={props.tool.failed}>
              <Icon name="alert" size={16} class="danger" />
            </Match>
          </Switch>
        </span>
        <span class="tool-title">{presentation().title}</span>
        <Show when={presentation().chip}>
          {(chip) => (
            <span class="chip" data-path={presentation().path || undefined}>
              <span>{chip()}</span>
            </span>
          )}
        </Show>
        <span class="facts">
          <Show when={props.tool.status === "notRun"}>
            <span class="fact muted">Not run</span>
          </Show>
          <For each={presentation().facts}>
            {(fact) => <span class={`fact ${fact.tone}`}>{fact.text}</span>}
          </For>
          <Show when={presentation().timed && presentation().facts.length === 0 && props.tool.startedAt}>
            <Duration tool={props.tool} />
          </Show>
        </span>
      </button>
      <Show when={open()}>
        <div class="tool-detail">
          <Show
            when={props.tool.output}
            fallback={<CodeBlock code={props.tool.arguments ?? ""} title="Arguments" />}
          >
            <CodeBlock code={props.tool.output} title={props.tool.failed ? "Error" : "Output"} />
          </Show>
          <ReplyAttachments items={resultFiles(props.tool)} />
        </div>
      </Show>
    </div>
  );
}

function Duration(props: { tool: ToolCall }) {
  const now = useClock();
  const ms = () => (props.tool.finishedAt ?? now()) - (props.tool.startedAt ?? now());
  return <span class="fact muted">{formatDuration(ms())}</span>;
}
