// The agents under the session's title: Main pinned at the start, then each
// subagent the session started. Choosing one shows its transcript in place.
import { createEffect, For, Match, on, Show, Switch } from "solid-js";
import type { SubagentConversation } from "../api/types";
import { Icon, Spinner } from "../ui/icons";
import type { Agents } from "./agents";
import { type AgentStatus, agentStatus } from "./roster";

const STATUS_WORDS: Record<AgentStatus, string> = {
  working: "working",
  queued: "queued",
  failed: "failed",
  interrupted: "interrupted",
  stopped: "stopped",
  done: "done",
};

export function AgentStrip(props: {
  agents: Agents;
  mainNeedsYou: boolean;
  /** Shows an agent's conversation, or Main's for undefined. */
  onShow(name: string | undefined): void;
}) {
  let scroller: HTMLDivElement | undefined;
  const fades = () => {
    if (!scroller) return;
    const { scrollLeft, scrollWidth, clientWidth } = scroller;
    scroller.toggleAttribute("data-before", scrollLeft > 1);
    scroller.toggleAttribute("data-after", scrollLeft + clientWidth < scrollWidth - 1);
  };
  // The agent in view scrolls into sight, also when the strip reorders;
  // Main brings back the start.
  createEffect(
    on(
      [
        () => props.agents.focused()?.agentName,
        () =>
          props.agents
            .strip()
            .map((a) => a.agentName)
            .join("\n"),
      ],
      ([name]) => {
        requestAnimationFrame(() => {
          if (!scroller) return;
          const button = name
            ? scroller.querySelector<HTMLElement>(`[data-agent="${CSS.escape(name)}"]`)
            : undefined;
          if (button) {
            const left = button.offsetLeft - (scroller.clientWidth - button.offsetWidth) / 2;
            scroller.scrollTo({ left, behavior: "smooth" });
          } else if (!name) {
            scroller.scrollTo({ left: 0, behavior: "smooth" });
          }
          fades();
        });
      },
    ),
  );
  return (
    <nav class="agent-strip" aria-label="Agents">
      <button
        type="button"
        class="agent-button"
        aria-pressed={props.agents.focused() === undefined}
        aria-label={props.mainNeedsYou ? "Main, needs you" : "Main"}
        onClick={() => props.onShow(undefined)}
      >
        <span>Main</span>
        <Show when={props.mainNeedsYou}>
          <span class="needs-dot" />
        </Show>
      </button>
      <div class="agent-scroll" ref={scroller} onScroll={fades}>
        <For each={props.agents.strip()}>
          {(agent) => (
            <AgentButton
              agent={agent}
              selected={props.agents.focused()?.id === agent.id}
              onClick={() => props.onShow(agent.agentName)}
            />
          )}
        </For>
      </div>
    </nav>
  );
}

function AgentButton(props: { agent: SubagentConversation; selected: boolean; onClick(): void }) {
  const status = () => agentStatus(props.agent);
  return (
    <button
      type="button"
      class="agent-button"
      data-agent={props.agent.agentName}
      data-status={status()}
      aria-pressed={props.selected}
      aria-label={`${props.agent.agentName}, ${STATUS_WORDS[status()]}`}
      onClick={() => props.onClick()}
    >
      <Switch>
        <Match when={status() === "working"}>
          <Spinner size={14} />
        </Match>
        <Match when={status() === "queued"}>
          <Icon name="clock" size={14} />
        </Match>
        <Match when={status() === "failed" || status() === "interrupted"}>
          <Icon name="alertCircle" size={14} />
        </Match>
        <Match when={status() === "stopped"}>
          <Icon name="minusCircle" size={14} />
        </Match>
        <Match when={status() === "done"}>
          <Icon name="checkCircle" size={14} />
        </Match>
      </Switch>
      <span>{props.agent.agentName}</span>
    </button>
  );
}
