// The new session sheet: a folder on the server, a model, a thinking level,
// and optionally a name and Temporary, as the Mac app's form asks.
import "@akonwi/mica/combobox.js";
import { createEffect, createMemo, createSignal, For, on, Show } from "solid-js";
import { ApiError } from "../api/client";
import type { ModelCapability, ServerSettings, SessionInfo } from "../api/types";
import { useKit, useReady } from "../app/kit";
import { navigate } from "../app/router";
import { serverUserHome } from "../ui/format";
import { Icon, Spinner } from "../ui/icons";
import { Modal } from "../ui/Modal";
import {
  createFailure,
  defaultThinking,
  definitelyRejected,
  folderOptions,
  folderPath,
  newId,
  preferredModel,
  recentFolders,
} from "./manage";
import { providerName, THINKING_LABELS } from "./models";

export function NewSessionSheet(props: { open: boolean; onClose(): void; sessions: readonly SessionInfo[] }) {
  const kit = useKit();
  const ready = useReady();
  const home = serverUserHome(ready.health.home);
  const folders = createMemo(() => recentFolders(props.sessions, home));

  const [folder, setFolder] = createSignal("");
  const [models, setModels] = createSignal<ModelCapability[]>();
  const [model, setModel] = createSignal("");
  const [thinking, setThinking] = createSignal("");
  const [name, setName] = createSignal("");
  const [temporary, setTemporary] = createSignal(false);
  const [error, setError] = createSignal("");
  const [folderError, setFolderError] = createSignal("");
  const [pending, setPending] = createSignal(false);
  // One id per form: a create whose answer was lost is looked up by it.
  let creationId = newId("session");

  const levels = () => models()?.find((value) => value.id === model())?.thinkingLevels ?? [];
  const available = createMemo(() => (models() ?? []).filter((value) => value.available));
  const groups = createMemo(() => {
    const byProvider = new Map<string, ModelCapability[]>();
    for (const value of available())
      byProvider.set(value.provider, [...(byProvider.get(value.provider) ?? []), value]);
    return [...byProvider.entries()];
  });
  const target = () => folderPath(folder(), home);
  const canCreate = () => Boolean(target()) && Boolean(model()) && !pending();

  const reset = () => {
    setFolder(folders()[0] ?? (home ? "~" : ""));
    setName("");
    setTemporary(false);
    setError("");
    setFolderError("");
    creationId = newId("session");
  };

  const load = async () => {
    reset();
    try {
      const catalog = (await ready.api.listModels()).models;
      let settings: ServerSettings | undefined;
      try {
        settings = await ready.api.serverSettings();
      } catch (failure) {
        if (kit.fail(failure)) return;
        setError("Couldn’t load the default model. Choose a model below.");
      }
      setModels(catalog);
      setModel(
        preferredModel(
          settings,
          catalog.filter((value) => value.available).map((value) => value.id),
        ),
      );
      setThinking(defaultThinking(levels()));
    } catch (failure) {
      if (!kit.fail(failure))
        setError(failure instanceof Error ? failure.message : "Models couldn’t be loaded.");
    }
  };

  const chooseModel = (id: string) => {
    setModel(id);
    if (!levels().includes(thinking())) setThinking(defaultThinking(levels()));
  };

  const create = async (event: SubmitEvent) => {
    event.preventDefault();
    const cwd = target();
    if (!cwd || !canCreate()) return;
    setPending(true);
    setError("");
    setFolderError("");
    try {
      const session = await ready.api.createSession({
        id: creationId,
        cwd,
        name: name().trim() || undefined,
        model: model(),
        thinkingLevel: levels().length > 0 ? thinking() : undefined,
        temporary: temporary() || undefined,
      });
      props.onClose();
      navigate(`/sessions/${encodeURIComponent(session.id)}`);
    } catch (failure) {
      if (kit.fail(failure)) return;
      if (definitelyRejected(failure)) {
        const message = createFailure(failure, folder(), kit.host);
        if (failure instanceof ApiError && message.startsWith("No folder")) setFolderError(message);
        else setError(message);
        return;
      }
      // The answer was lost; the session may exist under the id this form chose.
      try {
        await ready.api.getSession(creationId);
        props.onClose();
        navigate(`/sessions/${encodeURIComponent(creationId)}`);
      } catch {
        setError(
          "Kit may have created the session. Close this sheet and check the list before trying again.",
        );
      }
    } finally {
      setPending(false);
    }
  };

  // Each opening starts a fresh form.
  createEffect(
    on(
      () => props.open,
      (open) => {
        if (open) void load();
      },
    ),
  );

  return (
    <Modal
      open={props.open}
      onClose={props.onClose}
      drawer
      class="new-session"
      title="New session"
      detail={<span class="mono">{kit.host}</span>}
      footer={
        <button type="submit" form="new-session-form" data-variant="primary" disabled={!canCreate()}>
          <Show when={pending()}>
            <Spinner class="on-primary" />
          </Show>
          {pending() ? "Creating…" : "Create"}
        </button>
      }
    >
      <form id="new-session-form" class="form-stack" onSubmit={create}>
        <div class="field">
          <label for="new-session-folder">Folder</label>
          <m-combobox filter="manual">
            <input
              id="new-session-folder"
              class="mono"
              list="new-session-folders"
              autocomplete="off"
              autocapitalize="off"
              spellcheck={false}
              enterkeyhint="go"
              placeholder={home ? "~/path/to/folder" : "/path/to/folder"}
              aria-invalid={folderError() ? "true" : undefined}
              value={folder()}
              onInput={(event) => {
                setFolder(event.currentTarget.value);
                setFolderError("");
              }}
            />
            <Icon name="caretUpDown" size={16} class="muted combobox-caret" />
            <datalist id="new-session-folders">
              <For each={folderOptions(folders(), folder())}>{(value) => <option value={value} />}</For>
            </datalist>
            <p role="status">
              {home
                ? `Or type any path. ~ is your home folder on ${kit.host}.`
                : `Or type a full path on ${kit.host}.`}
            </p>
          </m-combobox>
          <Show when={folderError()}>
            <p class="field-error small" role="alert">
              <Icon name="alertCircle" size={16} />
              {folderError()}
            </p>
          </Show>
        </div>
        <div class="form-group">
          <label class="form-row">
            <span class="grow">Model</span>
            <Show when={models()} fallback={<span class="muted">Loading models…</span>}>
              <Show when={available().length > 0} fallback={<span class="muted">No models available</span>}>
                <select value={model()} onChange={(event) => chooseModel(event.currentTarget.value)}>
                  <For each={groups()}>
                    {([provider, list]) => (
                      <optgroup label={providerName(provider)}>
                        <For each={list}>{(value) => <option value={value.id}>{value.name}</option>}</For>
                      </optgroup>
                    )}
                  </For>
                </select>
              </Show>
            </Show>
          </label>
          <label class="form-row">
            <span class="grow">Thinking</span>
            <select
              value={thinking()}
              disabled={levels().length === 0}
              onChange={(event) => setThinking(event.currentTarget.value)}
            >
              <For each={levels()} fallback={<option value="">None</option>}>
                {(level) => <option value={level}>{THINKING_LABELS[level] ?? level}</option>}
              </For>
            </select>
          </label>
          <label class="form-row">
            <span class="form-label">Name</span>
            <input
              class="grow bare"
              placeholder="Optional"
              maxlength={256}
              value={name()}
              onInput={(event) => setName(event.currentTarget.value)}
            />
          </label>
          <label class="form-row">
            <span class="grow stacked">
              <span>Temporary session</span>
              <span class="muted small">
                Not saved. Gone when you delete it or {kit.host}’s server stops.
              </span>
            </span>
            <input
              type="checkbox"
              role="switch"
              aria-checked={temporary()}
              checked={temporary()}
              onChange={(event) => setTemporary(event.currentTarget.checked)}
            />
          </label>
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
