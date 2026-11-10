/**
 * Runs a Kit plugin's setup against an in-memory Kit, for tests.
 *
 * Copy this file beside kit-plugin.ts. It answers the plugin's requests as
 * Kit does, records what the plugin contributes, and lets a test call the
 * plugin's commands, tools, and interceptor and send it events.
 *
 * It rejects what Kit rejects for identifiers, required text, tool input
 * schemas, footer targets, URLs, and idempotency keys, and closes dialogs
 * when the request they serve ends. It does not apply Kit's byte and count
 * limits; run the plugin in Kit to check those.
 *
 * ```ts
 * import { expect, test } from "bun:test";
 * import { startTestPlugin } from "./kit-plugin-testing.ts";
 * import { setup } from "./plugin.ts";
 *
 * test("counts words", async () => {
 *   const kit = await startTestPlugin(setup);
 *   const result = await kit.callTool("word_count", { text: "one two" });
 *   expect(result.content).toEqual([{ type: "text", text: "2 words" }]);
 * });
 * ```
 */
import {
  type CommandRegisterParams,
  type ConfirmParams,
  connect,
  ErrorCode,
  type FooterSegment,
  type InitializeParams,
  type InputParams,
  type JsonValue,
  KitError,
  type KitEvents,
  type PluginRequests,
  type Project,
  type RequestID,
  type SelectParams,
  type Session,
  type Setup,
  type SubagentRegisterParams,
  type SubmitMessageParams,
  type ToastParams,
  type ToolCallDecision,
  type ToolExecuteResult,
  type ToolRegisterParams,
} from "./kit-plugin.ts";

/** A dialog the plugin opened. */
export type Dialog =
  | { method: "kit/ui/confirm"; params: ConfirmParams; signal: AbortSignal }
  | { method: "kit/ui/input"; params: InputParams; signal: AbortSignal }
  | { method: "kit/ui/select"; params: SelectParams; signal: AbortSignal };

/** How the in-memory Kit starts. */
export interface TestKitOptions {
  /** The plugin's ID, used in canonical identifiers. Defaults to `test`. */
  pluginId?: string;
  session?: Session;
  project?: Project;
  kitVersion?: string;
  instanceId?: string;
  /**
   * Answers a dialog: true or false for confirm, a string or null for input,
   * `{ value }` or null for select. The signal aborts when Kit would close the
   * dialog. Without it, every dialog is dismissed.
   */
  dialogs?: (dialog: Dialog) => unknown | Promise<unknown>;
  /**
   * Answers a plugin request before the in-memory Kit does. Return undefined
   * to let it answer, or throw a KitError to fail the request.
   */
  respond?: (method: string, params: unknown) => unknown | Promise<unknown>;
  /**
   * Whether startTestPlugin waits for setup to finish. Defaults to true; turn
   * it off to test a plugin stopped while setup is still running.
   */
  waitForSetup?: boolean;
}

/** Options of a request the test sends the plugin. */
export interface CallOptions {
  /** Aborting it sends kit/cancel; the call rejects with the abort reason. */
  signal?: AbortSignal;
}

/** The in-memory Kit, as a test sees it. */
export interface TestKit {
  /** Registered commands, by local ID. */
  readonly commands: ReadonlyMap<string, CommandRegisterParams>;
  /** Registered tools, by local ID. */
  readonly tools: ReadonlyMap<string, ToolRegisterParams>;
  /** Registered subagents, by local ID. */
  readonly subagents: ReadonlyMap<string, SubagentRegisterParams>;
  /** Footer items, by local ID. */
  readonly footer: ReadonlyMap<string, FooterSegment[]>;
  /** Footer items the plugin hides. */
  readonly hidden: ReadonlySet<string>;
  /** Whether the plugin has an interceptor. */
  readonly intercepting: boolean;
  readonly toasts: readonly ToastParams[];
  /** Messages the plugin submitted to the session. */
  readonly messages: readonly SubmitMessageParams[];
  /** Addresses the plugin asked clients to open. */
  readonly urls: readonly string[];
  /** Every dialog the plugin opened, in order. */
  readonly dialogs: readonly Dialog[];
  /** Every message the plugin wrote, in order. */
  readonly written: readonly JsonValue[];
  /** What the plugin wrote to its diagnostics. */
  readonly diagnostics: string;
  /** The plugin's exit code once it has exited. */
  readonly exitCode: number | undefined;

  /** Runs a command; rejects with a KitError when it fails. */
  runCommand(id: string, args?: string, options?: CallOptions): Promise<void>;
  /** Calls a tool as the model would. */
  callTool(id: string, input: JsonValue, options?: CallOptions & { toolCallId?: string }): Promise<ToolExecuteResult>;
  /** Asks the plugin's interceptor about a tool call. */
  intercept(
    call: { name: string; input: { [key: string]: JsonValue }; id?: string },
    options?: CallOptions,
  ): Promise<ToolCallDecision>;
  /** Sends an event and lets its listeners run. */
  emit<E extends keyof KitEvents>(event: E, params: KitEvents[E]): Promise<void>;
  /** Sends any message, such as a notification the helper does not know. */
  send(message: object): void;
  /**
   * Waits until no event listener is running and the plugin has stopped
   * writing. Calls to commands, tools, and the interceptor resolve when they
   * finish.
   */
  settle(): Promise<void>;
  /** Sends shutdown and waits for the plugin to exit. */
  shutdown(): Promise<void>;
  /** Closes the plugin's input, as Kit does when it goes away, and waits for it to exit. */
  close(): Promise<void>;
}

const eventMethods: Record<keyof KitEvents, string> = {
  "session.changed": "kit/events/session.changed",
  "project.changed": "kit/events/project.changed",
  "git.changed": "kit/events/git.changed",
  "turn.started": "kit/events/agent.turn.started",
  "turn.completed": "kit/events/agent.turn.completed",
};

const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const localId = /^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)*$/;
const toolId = /^[a-z][a-z0-9_]*$/;
const footerTarget = /^([a-z][a-z0-9-]{0,31})\.[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)*$/;
// The characters Kit treats as white space (Go's unicode.IsSpace), which
// differ from JavaScript's \s: U+0085 is one, U+FEFF is not.
const space = "\\t\\n\\v\\f\\r \\u0085\\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000";
const nonBlank = new RegExp(`[^${space}]`);
const spaceOrControl = new RegExp(`[${space}\\u0000-\\u001f\\u007f-\\u009f]`);
const idempotencyKey = /^[A-Za-z0-9._:-]{1,128}$/;
const schemaKeywords = new Set([
  "$schema",
  "type",
  "description",
  "properties",
  "required",
  "additionalProperties",
  "items",
  "enum",
  "const",
  "anyOf",
  "minimum",
  "maximum",
  "exclusiveMinimum",
  "exclusiveMaximum",
  "minLength",
  "maxLength",
  "pattern",
  "minItems",
  "maxItems",
]);

function invalid(rule: string): KitError {
  return new KitError(ErrorCode.InvalidParams, `Invalid parameter: ${rule}`);
}

function blank(text: unknown): boolean {
  return typeof text !== "string" || !nonBlank.test(text);
}

/** Checks a tool input schema against Kit's subset, as Kit does at registration. */
function checkSchema(node: unknown, depth: number): void {
  const isObject = (value: unknown) => !!value && typeof value === "object" && !Array.isArray(value);
  if (!isObject(node) || depth > 16) throw invalid("inputSchema");
  for (const [keyword, value] of Object.entries(node as object)) {
    if (!schemaKeywords.has(keyword)) throw invalid(`inputSchema keyword ${keyword}`);
    if (keyword === "additionalProperties" && typeof value !== "boolean") throw invalid("inputSchema additionalProperties");
    if (keyword === "properties") {
      if (!isObject(value)) throw invalid("inputSchema properties");
      for (const child of Object.values(value as object)) checkSchema(child, depth + 1);
    }
    if (keyword === "items") checkSchema(value, depth + 1);
    if (keyword === "anyOf") {
      if (!Array.isArray(value)) throw invalid("inputSchema anyOf");
      for (const child of value) checkSchema(child, depth + 1);
    }
  }
}

/** Rejects requests Kit would reject for the rules this file checks. */
function validate(method: string, params: unknown): void {
  const value = (params ?? {}) as Record<string, unknown>;
  const id = typeof value.id === "string" ? value.id : "";
  switch (method) {
    case "kit/commands/register":
    case "kit/subagents/register":
      if (!localId.test(id) || id.length > 128) throw invalid("id");
      if (blank(value.description)) throw invalid("description");
      if (method === "kit/subagents/register" && blank(value.instructions)) throw invalid("instructions");
      return;
    case "kit/commands/unregister":
    case "kit/subagents/unregister":
    case "kit/footer/clear":
      if (!localId.test(id) || id.length > 128) throw invalid("id");
      return;
    case "kit/tools/register": {
      if (!toolId.test(id) || id.length > 64) throw invalid("id");
      if (typeof value.description !== "string" || value.description === "") throw invalid("description");
      const schema = value.inputSchema as { type?: unknown } | undefined;
      if (!schema || schema.type !== "object") throw invalid("inputSchema root must have type object");
      checkSchema(schema, 0);
      return;
    }
    case "kit/tools/unregister":
      if (!toolId.test(id) || id.length > 64) throw invalid("id");
      return;
    case "kit/footer/set": {
      if (!localId.test(id) || id.length > 128) throw invalid("id");
      const content = value.content;
      if (!Array.isArray(content) || content.length < 1 || content.length > 32) throw invalid("content");
      return;
    }
    case "kit/footer/hide":
    case "kit/footer/show":
      if (id !== "kit.footer.location") {
        const plugin = footerTarget.exec(id)?.[1];
        if (!plugin || plugin === "kit" || plugin.startsWith("kit-")) throw invalid("id");
      }
      return;
    case "kit/ui/confirm":
    case "kit/ui/input":
    case "kit/ui/select":
      if (blank(value.title)) throw invalid("title");
      if (method === "kit/ui/select") {
        const options = value.options;
        if (!Array.isArray(options) || options.length < 1 || options.length > 64) throw invalid("options");
        if (options.some((option) => blank((option as { label?: unknown }).label))) throw invalid("label");
      }
      return;
    case "kit/session/submit-message":
      if (blank(value.text) || String(value.text).includes("\u0000")) throw invalid("text");
      if (value.idempotencyKey != null && !idempotencyKey.test(String(value.idempotencyKey))) {
        throw invalid("idempotencyKey");
      }
      return;
    case "kit/system/open-url": {
      const text = String(value.url ?? "");
      if (spaceOrControl.test(text)) throw invalid("url must not contain white space or controls");
      let url: URL;
      try {
        url = new URL(text);
      } catch {
        throw invalid("url must be an absolute URL");
      }
      if (url.protocol !== "http:" && url.protocol !== "https:") throw invalid("url must use http or https");
      if (!url.hostname) throw invalid("url must have a host");
      if (url.username || url.password) throw invalid("url must not contain credentials");
      return;
    }
  }
}

/** Starts setup against an in-memory Kit and resolves once setup has finished or the plugin exited. */
export async function startTestPlugin(setup: Setup, options: TestKitOptions = {}): Promise<TestKit> {
  const pluginId = options.pluginId ?? "test";
  const commands = new Map<string, CommandRegisterParams>();
  const tools = new Map<string, ToolRegisterParams>();
  const subagents = new Map<string, SubagentRegisterParams>();
  const footer = new Map<string, FooterSegment[]>();
  const hidden = new Set<string>();
  const toasts: ToastParams[] = [];
  const messages: SubmitMessageParams[] = [];
  const urls: string[] = [];
  const dialogs: Dialog[] = [];
  const written: JsonValue[] = [];
  let diagnostics = "";
  let exitCode: number | undefined;
  let intercepting = false;
  const keyed = new Map<string, { text: string; result: { messageId: string; turnId: string } }>();
  let next = 0;
  let submitted = 0;
  let markExited: () => void = () => {};
  const exited = new Promise<void>((resolve) => {
    markExited = resolve;
  });

  // Kit requests the plugin has not answered, and plugin requests Kit has not.
  const waiting = new Map<string, { resolve(value: unknown): void; reject(error: unknown): void }>();
  const serving = new Map<string, { cancel(): void; parentOf?: string }>();
  const key = (id: unknown) => JSON.stringify(id);

  const connection = connect(setup, {
    write(line) {
      const message = JSON.parse(line) as { id?: RequestID; method?: string; params?: unknown; result?: unknown; error?: { code: number; message: string; data?: JsonValue } };
      written.push(message as JsonValue);
      queueMicrotask(() => route(message));
    },
    log(text) {
      diagnostics += text;
    },
    exit(code) {
      exitCode ??= code;
      markExited();
    },
  });

  function deliver(message: object): void {
    connection.receive(JSON.stringify({ jsonrpc: "2.0", ...message }));
  }

  /** Ends dialogs that served Kit request parent, as Kit does when it ends. */
  function endChildren(parent: string): void {
    for (const entry of serving.values()) if (entry.parentOf === parent) entry.cancel();
  }

  function call(method: string, params: unknown, callOptions?: CallOptions): Promise<unknown> {
    const signal = callOptions?.signal;
    if (signal?.aborted) return Promise.reject(signal.reason);
    const id = `kit-${++next}`;
    return new Promise((resolve, reject) => {
      const onAbort = () => {
        if (!waiting.delete(key(id))) return;
        deliver({ method: "kit/cancel", params: { id } });
        endChildren(key(id));
        reject(signal?.reason);
      };
      signal?.addEventListener("abort", onAbort, { once: true });
      waiting.set(key(id), {
        resolve(value) {
          signal?.removeEventListener("abort", onAbort);
          resolve(value);
        },
        reject(error) {
          signal?.removeEventListener("abort", onAbort);
          reject(error);
        },
      });
      deliver({ id, method, params });
    });
  }

  function route(message: { id?: RequestID; method?: string; params?: unknown; result?: unknown; error?: { code: number; message: string; data?: JsonValue } }): void {
    if (message.method === undefined) {
      const pending = waiting.get(key(message.id));
      if (!pending) return;
      waiting.delete(key(message.id));
      endChildren(key(message.id));
      if (message.error) pending.reject(new KitError(message.error.code, message.error.message, message.error.data));
      else pending.resolve(message.result);
      return;
    }
    if (message.id === undefined) {
      if (message.method === "kit/ui/toast") {
        // Kit ignores an invalid toast.
        const toast = message.params as ToastParams;
        if (!blank(toast?.title) && ["info", "warning", "error"].includes(toast.variant)) toasts.push(toast);
      }
      if (message.method === "kit/cancel") serving.get(key((message.params as { id: RequestID }).id))?.cancel();
      return;
    }
    void serve(message.id, message.method, message.params);
  }

  async function serve(id: RequestID, method: string, params: unknown): Promise<void> {
    const controller = new AbortController();
    let settled = false;
    const reply = (body: object) => {
      if (settled) return;
      settled = true;
      serving.delete(key(id));
      deliver({ id, ...body });
    };
    const parent = (params as { parentId?: RequestID } | undefined)?.parentId;
    serving.set(key(id), {
      cancel() {
        controller.abort();
        reply({ error: { code: ErrorCode.Cancelled, message: "Cancelled" } });
      },
      ...(parent === undefined ? {} : { parentOf: key(parent) }),
    });
    if (parent !== undefined && !waiting.has(key(parent))) {
      serving.get(key(id))?.cancel();
      return;
    }
    try {
      let result = options.respond ? await options.respond(method, params) : undefined;
      if (result === undefined) result = await answer(method, params, controller.signal);
      reply({ result });
    } catch (error) {
      const code = error instanceof KitError ? error.code : ErrorCode.InternalError;
      const data = error instanceof KitError && error.data !== undefined ? { data: error.data } : {};
      reply({ error: { code, message: error instanceof Error ? error.message : String(error), ...data } });
    }
  }

  async function answer(method: string, params: unknown, signal: AbortSignal): Promise<unknown> {
    const local = (params as { id?: string } | undefined)?.id ?? "";
    const canonical = { id: `${pluginId}.${local}` };
    const conflict = () => new KitError(ErrorCode.Conflict, `${local} is already registered`);
    validate(method, params);
    switch (method as keyof PluginRequests) {
      case "kit/commands/register":
        if (commands.has(local)) throw conflict();
        commands.set(local, params as CommandRegisterParams);
        return canonical;
      case "kit/commands/unregister":
        commands.delete(local);
        return null;
      case "kit/tools/register":
        if (`${pluginId}__${local}`.length > 64) throw invalid("the tool's model-facing name exceeds 64 characters");
        if (tools.has(local)) throw conflict();
        tools.set(local, params as ToolRegisterParams);
        return { ...canonical, modelName: `${pluginId}__${local}` };
      case "kit/tools/unregister":
        tools.delete(local);
        return null;
      case "kit/tool-calls/register-interceptor":
        intercepting = true;
        return null;
      case "kit/tool-calls/unregister-interceptor":
        intercepting = false;
        return null;
      case "kit/subagents/register":
        if (subagents.has(local)) throw conflict();
        subagents.set(local, params as SubagentRegisterParams);
        return canonical;
      case "kit/subagents/unregister":
        subagents.delete(local);
        return null;
      case "kit/footer/set":
        footer.set(local, (params as { content: FooterSegment[] }).content);
        return canonical;
      case "kit/footer/clear":
        footer.delete(local);
        return null;
      case "kit/footer/hide":
        hidden.add(local);
        return null;
      case "kit/footer/show":
        hidden.delete(local);
        return null;
      case "kit/ui/confirm":
      case "kit/ui/input":
      case "kit/ui/select": {
        const dialog = { method, params, signal } as Dialog;
        dialogs.push(dialog);
        const dismissed = method === "kit/ui/confirm" ? false : null;
        const chosen = options.dialogs ? await options.dialogs(dialog) : dismissed;
        return chosen === undefined ? dismissed : chosen;
      }
      case "kit/session/submit-message": {
        // Kit records nothing for a message it rejects, so a respond hook
        // that rejects it keeps it out of messages.
        const message = params as SubmitMessageParams;
        const previous = message.idempotencyKey ? keyed.get(message.idempotencyKey) : undefined;
        if (previous) {
          if (previous.text !== message.text) {
            throw new KitError(ErrorCode.Conflict, "The idempotency key was used with different text");
          }
          return previous.result;
        }
        messages.push(message);
        submitted++;
        const result = { messageId: `message-${submitted}`, turnId: `turn-${submitted}` };
        if (message.idempotencyKey) keyed.set(message.idempotencyKey, { text: message.text, result });
        return result;
      }
      case "kit/system/open-url":
        urls.push((params as { url: string }).url);
        return null;
    }
    throw new KitError(ErrorCode.MethodNotFound, `Method not found: ${method}`);
  }

  const initialize: InitializeParams = {
    protocolVersion: 2,
    kit: { version: options.kitVersion ?? "test" },
    instance: { id: options.instanceId ?? `${pluginId}:1` },
    context: {
      session: options.session ?? { id: "session-test", name: null },
      project: options.project ?? { cwd: "/project", git: null },
    },
  };
  const initialized = call("initialize", initialize);
  if (options.waitForSetup !== false) {
    await Promise.race([initialized.then(() => Promise.race([connection.ready, exited])), exited]);
  } else {
    await Promise.race([initialized, exited]);
  }

  const settle = async () => {
    for (let before = -1; before !== written.length; ) {
      before = written.length;
      await tick();
      await connection.idle();
      await tick();
    }
  };

  return {
    commands,
    tools,
    subagents,
    footer,
    hidden,
    get intercepting() {
      return intercepting;
    },
    toasts,
    messages,
    urls,
    dialogs,
    written,
    get diagnostics() {
      return diagnostics;
    },
    get exitCode() {
      return exitCode;
    },
    runCommand: async (id, args = "", callOptions) => {
      await call("kit/commands/execute", { id, args }, callOptions);
    },
    callTool: async (id, input, callOptions) =>
      (await call(
        "kit/tools/execute",
        { id, toolCallId: callOptions?.toolCallId ?? `call-${next + 1}`, input },
        callOptions,
      )) as ToolExecuteResult,
    intercept: async (toolCall, callOptions) =>
      (await call(
        "kit/tool-calls/before-execute",
        { toolCall: { id: toolCall.id ?? `call-${next + 1}`, name: toolCall.name, input: toolCall.input } },
        callOptions,
      )) as ToolCallDecision,
    async emit(event, params) {
      deliver({ method: eventMethods[event], params });
      await settle();
    },
    send: (message) => deliver(message),
    settle,
    async shutdown() {
      await call("shutdown", undefined);
      await exited;
    },
    async close() {
      connection.close();
      await exited;
    },
  };
}
