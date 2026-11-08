// The session menu's actions beyond model and thinking: rename, fork, the
// details sheet, and delete, each in its own dialog.
import { createEffect, createSignal, on, Show } from "solid-js";
import type { Api } from "../api/client";
import { useKit } from "../app/kit";
import { navigate } from "../app/router";
import { displayPath, tokens } from "../ui/format";
import { Icon, Spinner } from "../ui/icons";
import { Modal } from "../ui/Modal";
import { deleteFailure, deleteWarning } from "./manage";
import { THINKING_LABELS } from "./models";
import type { SessionState } from "./projection";

export type SessionDialog = "rename" | "fork" | "details" | "delete";

interface DialogProps {
  api: Api;
  session: SessionState;
  open: boolean;
  onClose(): void;
}

/** Where a fork whose first message didn't start leaves that message for its composer. */
export const FORK_FAILURE_KEY = "kit.forkFailure:";

export function RenameDialog(props: DialogProps) {
  const kit = useKit();
  const [name, setName] = createSignal("");
  const [error, setError] = createSignal("");
  const [pending, setPending] = createSignal(false);
  createEffect(
    on(
      () => props.open,
      (open) => {
        if (!open) return;
        setName(props.session.info.name ?? "");
        setError("");
      },
    ),
  );
  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    const value = name().trim();
    if (!value || pending()) return;
    setPending(true);
    setError("");
    try {
      await props.api.renameSession(props.session.info.id, value);
      props.onClose();
    } catch (failure) {
      if (!kit.fail(failure))
        setError(failure instanceof Error ? failure.message : "The name didn’t change.");
    } finally {
      setPending(false);
    }
  };
  return (
    <Modal
      open={props.open}
      onClose={props.onClose}
      drawer
      title="Rename session"
      footer={
        <button
          type="submit"
          form="rename-form"
          data-variant="primary"
          disabled={!name().trim() || pending()}
        >
          {pending() ? "Renaming…" : "Rename"}
        </button>
      }
    >
      <form id="rename-form" class="form-stack" onSubmit={submit}>
        <div class="field">
          <label for="rename-name">Name</label>
          <input
            id="rename-name"
            maxlength={256}
            enterkeyhint="done"
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
          />
        </div>
        <Show when={error()}>
          <p class="error small" role="alert">
            {error()}
          </p>
        </Show>
      </form>
    </Modal>
  );
}

export function ForkDialog(props: DialogProps) {
  const kit = useKit();
  const [name, setName] = createSignal("");
  const [message, setMessage] = createSignal("");
  const [error, setError] = createSignal("");
  const [pending, setPending] = createSignal(false);
  createEffect(
    on(
      () => props.open,
      (open) => {
        if (!open) return;
        setName("");
        setMessage("");
        setError("");
      },
    ),
  );
  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    if (pending()) return;
    setPending(true);
    setError("");
    const text = message().trim();
    try {
      const result = await props.api.forkSession(props.session.info.id, {
        name: name().trim() || undefined,
        prompt: text ? { text } : undefined,
      });
      if (result.firstTurnError) {
        sessionStorage.setItem(
          FORK_FAILURE_KEY + result.session.id,
          JSON.stringify({ text, message: result.firstTurnError.message }),
        );
      }
      props.onClose();
      navigate(`/sessions/${encodeURIComponent(result.session.id)}`);
    } catch (failure) {
      if (!kit.fail(failure))
        setError(failure instanceof Error ? failure.message : "The session wasn’t forked.");
    } finally {
      setPending(false);
    }
  };
  return (
    <Modal
      open={props.open}
      onClose={props.onClose}
      drawer
      title="Fork session"
      detail="Continue from this session’s current history in a separate session. A first message starts the fork’s first turn."
      footer={
        <button type="submit" form="fork-form" data-variant="primary" disabled={pending()}>
          <Show when={pending()}>
            <Spinner class="on-primary" />
          </Show>
          {pending() ? "Forking…" : error() ? "Retry" : "Fork"}
        </button>
      }
    >
      <form id="fork-form" class="form-stack" onSubmit={submit}>
        <div class="field">
          <label for="fork-name">Name</label>
          <input
            id="fork-name"
            maxlength={256}
            placeholder="Optional"
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
          />
        </div>
        <div class="field">
          <label for="fork-message">First message</label>
          <textarea
            id="fork-message"
            rows={3}
            placeholder="Optional"
            value={message()}
            onInput={(event) => setMessage(event.currentTarget.value)}
          />
          <p class="hint">Leave empty to fork without starting a turn.</p>
        </div>
        <Show when={error()}>
          <p class="error small" role="alert">
            {error()}
          </p>
        </Show>
      </form>
    </Modal>
  );
}

const DATE_TIME = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
const COST = new Intl.NumberFormat(undefined, { style: "currency", currency: "USD" });

export function DetailsSheet(props: DialogProps & { home?: string; modelName?: string }) {
  const [copied, setCopied] = createSignal(false);
  const percent = () => {
    const used = props.session.contextTokens;
    const window = props.session.contextWindow;
    return used !== undefined && window ? Math.min(100, Math.round((used / window) * 100)) : undefined;
  };
  const started = () => {
    const date = new Date(props.session.info.createdAt);
    return Number.isNaN(date.getTime()) ? "" : DATE_TIME.format(date);
  };
  const canCopy = Boolean(navigator.clipboard);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(props.session.info.id);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // A refused write leaves the id to select by hand.
    }
  };
  const usage = () => props.session.usage;
  const thinking = () =>
    THINKING_LABELS[props.session.info.thinkingLevel] ?? props.session.info.thinkingLevel;
  return (
    <Modal
      open={props.open}
      onClose={props.onClose}
      drawer
      class="details-sheet"
      title={props.session.info.name || "Untitled session"}
      detail={<span class="mono">{displayPath(props.session.info.cwd, props.home)}</span>}
    >
      <div class="form-stack">
        <Show when={percent() !== undefined}>
          <section class="detail-section">
            <h3 class="eyebrow">Context</h3>
            <div class="detail-card">
              <span class="line">
                <span class="grow">{percent()}% used</span>
                <span class="mono muted small">
                  {tokens(props.session.contextTokens ?? 0)} / {tokens(props.session.contextWindow ?? 0)}
                </span>
              </span>
              <meter min={0} max={100} value={percent()} aria-label="Context used" />
            </div>
          </section>
        </Show>
        <section class="detail-section">
          <h3 class="eyebrow">Usage</h3>
          <dl class="detail-card usage">
            <div>
              <dt>Input</dt>
              <dd class="mono">{tokens(usage().input)}</dd>
            </div>
            <div>
              <dt>Output</dt>
              <dd class="mono">{tokens(usage().output)}</dd>
            </div>
            <div>
              <dt>Cache read</dt>
              <dd class="mono">{tokens(usage().cacheRead)}</dd>
            </div>
            <div>
              <dt>Cost</dt>
              <dd class="mono">{COST.format(usage().cost.total)}</dd>
            </div>
          </dl>
        </section>
        <section class="detail-section">
          <h3 class="eyebrow">Session</h3>
          <dl class="detail-card rows">
            <div>
              <dt>Model</dt>
              <dd>
                {props.modelName ?? props.session.info.model}
                <Show when={thinking()}> · {thinking()}</Show>
              </dd>
            </div>
            <Show when={started()}>
              <div>
                <dt>Started</dt>
                <dd>{started()}</dd>
              </div>
            </Show>
            <Show when={props.session.info.temporary}>
              <div>
                <dt>Saved</dt>
                <dd>No, temporary</dd>
              </div>
            </Show>
            <div>
              <dt>ID</dt>
              <dd class="id">
                {/* The clipboard needs a secure page; without it the whole
                    id shows, selected with a tap. */}
                <span class="mono small" data-whole={canCopy ? undefined : ""}>
                  {props.session.info.id}
                </span>
                <Show when={canCopy}>
                  <button type="button" class="icon-button small" aria-label="Copy session ID" onClick={copy}>
                    <Icon name={copied() ? "check" : "copy"} size={16} />
                  </button>
                </Show>
              </dd>
            </div>
          </dl>
        </section>
      </div>
    </Modal>
  );
}

export function DeleteDialog(props: DialogProps) {
  const kit = useKit();
  const [error, setError] = createSignal("");
  const [pending, setPending] = createSignal(false);
  createEffect(
    on(
      () => props.open,
      (open) => {
        if (open) setError("");
      },
    ),
  );
  const remove = async () => {
    if (pending()) return;
    setPending(true);
    setError("");
    try {
      if (props.session.info.temporary) await props.api.disposeSession(props.session.info.id);
      else await props.api.deleteSession(props.session.info.id);
      props.onClose();
      navigate("/", { replace: true });
    } catch (failure) {
      if (!kit.fail(failure)) setError(deleteFailure(failure));
    } finally {
      setPending(false);
    }
  };
  return (
    <Modal
      open={props.open}
      onClose={props.onClose}
      alert
      title="Delete session?"
      footer={
        <>
          <button type="button" disabled={pending()} onClick={props.onClose}>
            Cancel
          </button>
          <button type="button" data-variant="danger" disabled={pending()} onClick={remove}>
            {pending() ? "Deleting…" : "Delete session"}
          </button>
        </>
      }
    >
      <div class="form-stack">
        <p class="muted">{deleteWarning(props.session.info)}</p>
        <Show when={error()}>
          <p class="error small" role="alert">
            {error()}
          </p>
        </Show>
      </div>
    </Modal>
  );
}
