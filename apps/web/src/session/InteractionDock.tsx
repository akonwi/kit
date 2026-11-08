// Answers the request a run is waiting on: a confirmation, a value, a
// choice, guided questions, or an authorization page.
import { createMemo, createSignal, For, Match, Show, Switch } from "solid-js";
import type { Api } from "../api/client";
import type { InteractionRequest, InteractionResponse } from "../api/types";
import { safeHref } from "../markdown/parse";
import { NeedsBadge } from "../ui/icons";

type Question = NonNullable<InteractionRequest["questions"]>[number];
type Answer = NonNullable<InteractionResponse["answers"]>[string];

export interface DockProps {
  api: Api;
  sessionId: string;
  interaction: InteractionRequest;
  fail(error: unknown): boolean;
}

export function InteractionDock(props: DockProps) {
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");

  const respond = async (response: Omit<InteractionResponse, "requestId">) => {
    if (busy()) return;
    setBusy(true);
    setError("");
    try {
      await props.api.respondInteraction(props.sessionId, props.interaction.id, {
        requestId: props.interaction.id,
        ...response,
      });
    } catch (failure) {
      if (!props.fail(failure))
        setError(failure instanceof Error ? failure.message : "That answer didn’t go through.");
      setBusy(false);
    }
  };

  const cancel = () => respond({ cancelled: true });

  return (
    <section class="dock" aria-label="Waiting for you">
      <Switch>
        <Match when={props.interaction.kind === "guided"}>
          <Guided interaction={props.interaction} busy={busy()} respond={respond} cancel={cancel} />
        </Match>
        <Match when={true}>
          <div class="dock-title">
            <NeedsBadge />
            <h2>{props.interaction.title}</h2>
          </div>
          <Show when={props.interaction.detail}>
            <p class="dock-detail">{props.interaction.detail}</p>
          </Show>
          <Switch>
            <Match when={props.interaction.kind === "confirm"}>
              <div class="dock-actions">
                <button type="button" disabled={busy()} onClick={() => respond({ confirmed: false })}>
                  {props.interaction.cancelLabel || "Deny"}
                </button>
                <button
                  type="button"
                  data-variant="primary"
                  disabled={busy()}
                  onClick={() => respond({ confirmed: true })}
                >
                  {props.interaction.confirmLabel || "Approve"}
                </button>
              </div>
            </Match>
            <Match when={props.interaction.kind === "select"}>
              <Choice interaction={props.interaction} busy={busy()} respond={respond} cancel={cancel} />
            </Match>
            <Match when={props.interaction.kind === "input" || props.interaction.kind === "authorize"}>
              <Value interaction={props.interaction} busy={busy()} respond={respond} cancel={cancel} />
            </Match>
            <Match when={true}>
              <p class="dock-detail">This request can’t be answered in the browser.</p>
              <div class="dock-actions">
                <button type="button" disabled={busy()} onClick={cancel}>
                  Cancel
                </button>
              </div>
            </Match>
          </Switch>
        </Match>
      </Switch>
      <Show when={error()}>
        <p class="error small" role="alert">
          {error()}
        </p>
      </Show>
    </section>
  );
}

interface AnswerProps {
  interaction: InteractionRequest;
  busy: boolean;
  respond(response: Omit<InteractionResponse, "requestId">): void;
  cancel(): void;
}

function Options(props: {
  name: string;
  options: NonNullable<InteractionRequest["options"]>;
  selected: string[];
  multiple?: boolean;
  onChange(selected: string[]): void;
}) {
  return (
    <div class="options" role={props.multiple ? "group" : "radiogroup"}>
      <For each={props.options}>
        {(option) => (
          <label class="option" data-selected={props.selected.includes(option.id) || undefined}>
            <input
              type={props.multiple ? "checkbox" : "radio"}
              name={props.name}
              value={option.id}
              checked={props.selected.includes(option.id)}
              onChange={(event) => {
                if (!props.multiple) props.onChange([option.id]);
                else if (event.currentTarget.checked) props.onChange([...props.selected, option.id]);
                else props.onChange(props.selected.filter((id) => id !== option.id));
              }}
            />
            <span>
              <span class="option-label">{option.label}</span>
              <Show when={option.detail}>
                <span class="option-detail">{option.detail}</span>
              </Show>
            </span>
          </label>
        )}
      </For>
    </div>
  );
}

function Choice(props: AnswerProps) {
  const [selected, setSelected] = createSignal<string[]>([]);
  return (
    <>
      <Options
        name={`choice-${props.interaction.id}`}
        options={props.interaction.options ?? []}
        selected={selected()}
        onChange={setSelected}
      />
      <div class="dock-actions">
        <button type="button" disabled={props.busy} onClick={props.cancel}>
          {props.interaction.cancelLabel || "Cancel"}
        </button>
        <button
          type="button"
          data-variant="primary"
          disabled={props.busy || selected().length === 0}
          onClick={() => props.respond({ selectedOptionId: selected()[0] })}
        >
          {props.interaction.confirmLabel || "Choose"}
        </button>
      </div>
    </>
  );
}

function Value(props: AnswerProps) {
  const [value, setValue] = createSignal(props.interaction.initialValue ?? "");
  const authorize = () => props.interaction.kind === "authorize";
  const page = () => (authorize() ? safeHref(props.interaction.url ?? "") : undefined);
  return (
    <form
      class="dock-form"
      onSubmit={(event) => {
        event.preventDefault();
        props.respond({ value: value() });
      }}
    >
      <Show when={page()}>
        {(href) => (
          <a class="btn" href={href()} target="_blank" rel="noopener noreferrer">
            Open the sign-in page
          </a>
        )}
      </Show>
      <textarea
        rows={authorize() ? 2 : 3}
        aria-label={authorize() ? "Address or code from the sign-in page" : props.interaction.title}
        placeholder={
          authorize()
            ? "Paste the address the page went to, or the code it showed"
            : (props.interaction.placeholder ?? "")
        }
        value={value()}
        onInput={(event) => setValue(event.currentTarget.value)}
      />
      <div class="dock-actions">
        <button type="button" disabled={props.busy} onClick={props.cancel}>
          {props.interaction.cancelLabel || "Cancel"}
        </button>
        <button
          type="submit"
          data-variant="primary"
          disabled={props.busy || (authorize() && !value().trim())}
        >
          {props.interaction.confirmLabel || "Send"}
        </button>
      </div>
    </form>
  );
}

function Guided(props: AnswerProps) {
  const questions = () => props.interaction.questions ?? [];
  const [step, setStep] = createSignal(0);
  const [answers, setAnswers] = createSignal<Record<string, Answer>>({});
  const question = createMemo<Question | undefined>(() => questions()[step()]);
  const last = () => step() >= questions().length - 1;

  const draft = (current: Question): Answer | undefined => answers()[current.id];
  const set = (current: Question, answer: Answer) => setAnswers({ ...answers(), [current.id]: answer });
  const answered = (current: Question): boolean => {
    const answer = draft(current);
    if (!answer) return current.kind === "boolean";
    if (answer.text !== undefined) return answer.text.trim() !== "";
    if (answer.optionIds !== undefined) return answer.optionIds.length > 0;
    return true;
  };

  const advance = (skip: boolean) => {
    const current = question();
    if (!current) return;
    let next = answers();
    if (skip) next = { ...next, [current.id]: { skipped: true } };
    else if (current.kind === "boolean" && !next[current.id])
      next = { ...next, [current.id]: { boolean: false } };
    setAnswers(next);
    if (last()) props.respond({ answers: next });
    else setStep(step() + 1);
  };

  return (
    <Show when={question()}>
      {(current) => (
        <>
          <div class="dock-title stacked">
            <div class="dock-step">
              <NeedsBadge />
              <span class="muted small">
                Question {step() + 1} of {questions().length}
              </span>
            </div>
            <h2>{current().prompt}</h2>
          </div>
          <Show when={current().detail}>
            <p class="dock-detail">{current().detail}</p>
          </Show>
          <Switch>
            <Match when={current().kind === "select" || current().kind === "multiselect"}>
              <Options
                name={`question-${current().id}`}
                options={current().options ?? []}
                multiple={current().kind === "multiselect"}
                selected={draft(current())?.optionIds ?? []}
                onChange={(ids) => set(current(), { optionIds: ids })}
              />
            </Match>
            <Match when={current().kind === "boolean"}>
              <Options
                name={`question-${current().id}`}
                options={[
                  { id: "yes", label: "Yes" },
                  { id: "no", label: "No" },
                ]}
                selected={draft(current())?.boolean === true ? ["yes"] : ["no"]}
                onChange={(ids) => set(current(), { boolean: ids[0] === "yes" })}
              />
            </Match>
            <Match when={true}>
              <textarea
                rows={3}
                aria-label={current().prompt}
                value={draft(current())?.text ?? ""}
                onInput={(event) => set(current(), { text: event.currentTarget.value })}
              />
            </Match>
          </Switch>
          <div class="dock-actions">
            <Show
              when={!current().required}
              fallback={
                <button type="button" disabled={props.busy} onClick={props.cancel}>
                  Cancel
                </button>
              }
            >
              <button type="button" disabled={props.busy} onClick={() => advance(true)}>
                Skip
              </button>
            </Show>
            <button
              type="button"
              data-variant="primary"
              disabled={props.busy || !answered(current())}
              onClick={() => advance(false)}
            >
              {last() ? "Send" : "Next"}
            </button>
          </div>
        </>
      )}
    </Show>
  );
}
