// The model and thinking lists a menu shows: the session menu for the
// session, the agent menu for a subagent.
import { createMemo, For, Show } from "solid-js";
import type { ModelCapability } from "../api/types";
import { Icon } from "../ui/icons";
import { providerName, THINKING_LABELS } from "./models";

/** The catalog by provider; a model that isn't available shows only while it's chosen. */
export function ModelChoices(props: {
  models: readonly ModelCapability[];
  current: string;
  disabled(model: ModelCapability): boolean;
  onChoose(model: ModelCapability): void;
}) {
  const groups = createMemo(() => {
    const byProvider = new Map<string, ModelCapability[]>();
    for (const model of props.models) {
      if (!model.available && model.id !== props.current) continue;
      byProvider.set(model.provider, [...(byProvider.get(model.provider) ?? []), model]);
    }
    return [...byProvider.entries()];
  });
  return (
    <div class="menu-scroll">
      <For each={groups()}>
        {([provider, list]) => (
          <>
            <span class="eyebrow menu-group">{providerName(provider)}</span>
            <For each={list}>
              {(model) => (
                <button
                  type="button"
                  class="menu-row"
                  aria-current={model.id === props.current || undefined}
                  disabled={props.disabled(model)}
                  onClick={() => props.onChoose(model)}
                >
                  <span class="grow">
                    <span>{model.name}</span>
                    <span class="mono muted small">{model.id.replace(/^[^/]+\//, "")}</span>
                  </span>
                  <Show when={model.id === props.current}>
                    <Icon name="check" size={16} class="accent" />
                  </Show>
                </button>
              )}
            </For>
          </>
        )}
      </For>
    </div>
  );
}

export function ThinkingChoices(props: {
  levels: readonly string[];
  current: string;
  disabled: boolean;
  onChoose(level: string): void;
}) {
  return (
    <For each={props.levels}>
      {(level) => (
        <button
          type="button"
          class="menu-row"
          aria-current={level === props.current || undefined}
          disabled={props.disabled}
          onClick={() => props.onChoose(level)}
        >
          <span class="grow">{THINKING_LABELS[level] ?? level}</span>
          <Show when={level === props.current}>
            <Icon name="check" size={16} class="accent" />
          </Show>
        </button>
      )}
    </For>
  );
}
