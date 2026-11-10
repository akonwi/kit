/**
 * Kit plugin helper for plugin protocol version 2.
 *
 * Copy this file into a plugin's source; it has no dependencies and runs under
 * Bun, and under Node with TypeScript type stripping. Take it from the Kit
 * repository at the release tag of the Kit you run:
 *
 *   https://github.com/akonwi/kit/blob/<tag>/sdk/plugin/kit-plugin.ts
 *
 * Protocol version 2 lets an older copy keep working with a newer Kit. The
 * protocol is specified in docs/plugin-protocol/v2.md.
 *
 * ```ts
 * import { runPlugin, t, type Kit } from "./kit-plugin.ts";
 *
 * export async function setup(kit: Kit) {
 *   await kit.tools.register({
 *     id: "word_count",
 *     description: "Count the words in a text",
 *     input: t.object({ text: t.string() }),
 *     execute: (input) => `${input.text.split(/\s+/).filter(Boolean).length} words`,
 *   });
 * }
 *
 * if (import.meta.main) runPlugin(setup);
 * ```
 *
 * `import.meta.main` lets tests import `setup` without starting the plugin;
 * Node supports it from 24.2, so on older Node call `runPlugin` from a
 * separate entry file.
 */

/** The plugin protocol version this file speaks. */
export const PROTOCOL_VERSION = 2;

/**
 * The Kit release this file belongs to, such as `"0.59.0"`. Each release sets
 * it, so a copy taken from tag `vX.Y.Z` says `"X.Y.Z"`; it is `"unreleased"`
 * before the first release that includes the helper.
 */
export const KIT_RELEASE: string = "0.59.0";

/** How long stopping waits for a setup that is still running. */
const SETUP_STOP_GRACE_MS = 1500;

// ---------------------------------------------------------------------------
// Errors

/** An error answer from Kit, or one a handler throws to choose its code. */
export class KitError extends Error {
  readonly code: number;
  readonly data: JsonValue | undefined;

  constructor(code: number, message: string, data?: JsonValue) {
    super(message);
    this.name = "KitError";
    this.code = code;
    this.data = data;
  }

  /** The `data.reason` Kit gave, such as `session_busy`. */
  get reason(): ErrorReason | undefined {
    const data = this.data;
    if (data && typeof data === "object" && !Array.isArray(data) && typeof data.reason === "string") {
      return data.reason as ErrorReason;
    }
    return undefined;
  }
}

/** Whether error is Kit's "try again later" answer, optionally for one reason. */
export function isTryLater(error: unknown, reason?: ErrorReason): error is KitError {
  return error instanceof KitError && error.code === ErrorCode.TryLater && (!reason || error.reason === reason);
}

// ---------------------------------------------------------------------------
// Tool input schemas

declare const inputType: unique symbol;

/** A tool input schema whose accepted values have type T. */
export interface Schema<T> {
  readonly schema: ToolSchemaNode;
  readonly [inputType]?: T;
}

/** An object property that may be absent. */
export interface OptionalSchema<T> extends Schema<T> {
  readonly optional: true;
}

/** The type of values a schema accepts. */
export type Infer<S> = S extends Schema<infer T> ? T : never;

type AnySchema = Schema<unknown>;
type ObjectInput<P extends Record<string, AnySchema>> = {
  [K in keyof P as P[K] extends OptionalSchema<unknown> ? never : K]: Infer<P[K]>;
} & {
  [K in keyof P as P[K] extends OptionalSchema<unknown> ? K : never]?: Infer<P[K]>;
};
type Simplify<T> = { [K in keyof T]: T[K] } & {};

interface Described {
  description?: string;
}
interface StringOptions extends Described {
  minLength?: number;
  maxLength?: number;
  /** A regular expression in RE2 syntax. */
  pattern?: string;
}
interface NumberOptions extends Described {
  minimum?: number;
  maximum?: number;
  exclusiveMinimum?: number;
  exclusiveMaximum?: number;
}
interface ArrayOptions extends Described {
  minItems?: number;
  maxItems?: number;
}
interface ObjectOptions extends Described {
  /** Whether members other than the declared properties are allowed. Defaults to false. */
  additionalProperties?: boolean;
}

function node<T>(schema: ToolSchemaNode): Schema<T> {
  return { schema };
}

function clean<O extends object>(options: O | undefined): Partial<O> {
  const result: Partial<O> = {};
  for (const [key, value] of Object.entries(options ?? {})) {
    if (value !== undefined) (result as Record<string, unknown>)[key] = value;
  }
  return result;
}

/**
 * Builds tool input schemas from the keywords Kit's tool schema subset allows,
 * and infers the input type a tool's handler receives.
 */
export const t = {
  string: <const S extends string = string>(options?: StringOptions): Schema<S> =>
    node({ type: "string", ...clean(options) }),
  number: (options?: NumberOptions): Schema<number> => node({ type: "number", ...clean(options) }),
  integer: (options?: NumberOptions): Schema<number> => node({ type: "integer", ...clean(options) }),
  boolean: (options?: Described): Schema<boolean> => node({ type: "boolean", ...clean(options) }),
  null: (options?: Described): Schema<null> => node({ type: "null", ...clean(options) }),
  /** One of the given strings, numbers, booleans, or null. */
  enum: <const V extends readonly (string | number | boolean | null)[]>(values: V, options?: Described): Schema<V[number]> =>
    node({ enum: [...values], ...clean(options) }),
  /** Exactly the given value. */
  literal: <const V extends string | number | boolean | null>(value: V, options?: Described): Schema<V> =>
    node({ const: value, ...clean(options) }),
  array: <S extends AnySchema>(items: S, options?: ArrayOptions): Schema<Infer<S>[]> =>
    node({ type: "array", items: items.schema, ...clean(options) }),
  object: <P extends Record<string, AnySchema>>(properties: P, options?: ObjectOptions): Schema<Simplify<ObjectInput<P>>> => {
    const required = Object.keys(properties).filter((name) => !("optional" in (properties[name] as object)));
    // No prototype, so a property named __proto__ is an ordinary property.
    const schemas: Record<string, ToolSchemaNode> = Object.create(null);
    for (const [name, property] of Object.entries(properties)) schemas[name] = property.schema;
    const { additionalProperties = false, ...rest } = clean(options);
    return node({ type: "object", properties: schemas, required, additionalProperties, ...rest });
  },
  /** Any of the given schemas. */
  union: <const S extends readonly AnySchema[]>(schemas: S, options?: Described): Schema<Infer<S[number]>> =>
    node({ anyOf: schemas.map((schema) => schema.schema), ...clean(options) }),
  /** The schema's values, or null. */
  nullable: <T>(schema: Schema<T>): Schema<T | null> => node({ anyOf: [schema.schema, { type: "null" }] }),
  /** Marks an object property that may be absent. */
  optional: <T>(schema: Schema<T>): OptionalSchema<T> => ({ schema: schema.schema, optional: true }),
  /**
   * A schema written by hand, with the input type it accepts. Kit rejects
   * keywords outside its subset when the tool is registered.
   */
  raw: <T>(schema: ToolInputSchema): Schema<T> => node(schema as ToolSchemaNode),
};

// ---------------------------------------------------------------------------
// The plugin API

/** Runs when the plugin stops. */
export type Disposer = () => void | Promise<void>;

/** A plugin's entry point. Kit calls it after initialization. */
export type Setup = (kit: Kit) => void | Disposer | Promise<void | Disposer>;

/** Options of a request a plugin sends Kit. */
export interface RequestOptions {
  /** Aborting it cancels the request; the promise rejects with the abort reason. */
  signal?: AbortSignal;
}

type DialogOptions<P> = Omit<P, "parentId">;

/** An option of a select dialog. */
export interface Option<V extends JsonValue> {
  label: string;
  value: V;
  description?: string | null;
}

/** Dialogs Kit shows in every client attached to the session. */
export interface Dialogs {
  /** Resolves to true, or false when the user declines or dismisses it. */
  confirm(options: DialogOptions<ConfirmParams>, request?: RequestOptions): Promise<boolean>;
  /** Resolves to the text entered, or null when the user dismisses it. */
  input(options: DialogOptions<InputParams>, request?: RequestOptions): Promise<string | null>;
  /** Resolves to the chosen option's value, or null when the user dismisses it. */
  select<const O extends readonly Option<JsonValue>[]>(
    options: Omit<DialogOptions<SelectParams>, "options"> & { options: O },
    request?: RequestOptions,
  ): Promise<{ value: O[number]["value"] } | null>;
}

/** The context of a request Kit sent the plugin. */
export interface RequestContext {
  /** Aborts when Kit cancels the request. */
  signal: AbortSignal;
  /** Dialogs that Kit closes when this request ends. */
  ui: Dialogs;
}

/** The context of a tool call. */
export interface ToolContext extends RequestContext {
  toolCallId: string;
}

/** A registered contribution. */
export interface Handle {
  /** The canonical identifier, prefixed with the plugin's ID. */
  readonly id: string;
  /** The identifier the plugin registered. */
  readonly localId: string;
  /** Removes the contribution. Removing it again succeeds. */
  unregister(request?: RequestOptions): Promise<void>;
}

/** A registered tool. */
export interface ToolHandle extends Handle {
  /** The name the model calls the tool by, such as `plugin__tool`. */
  readonly modelName: string;
}

/**
 * A tool's result. `details` is any value JSON can encode, such as a plugin's
 * own state types; Kit keeps it with the call and does not show it to the
 * model.
 */
export interface ToolResult extends Omit<ToolExecuteResult, "details"> {
  details?: unknown;
}

/** What a tool handler returns: a full result, or a string as its single text part. */
export type ToolOutcome = string | ToolResult;

/** A tool, with its handler. */
export interface ToolDefinition<I> extends Omit<ToolRegisterParams, "inputSchema" | "id"> {
  /** The tool's local ID: lower case letters, digits, and underscores. */
  id: string;
  /** The tool's input schema, from `t`. */
  input: Schema<I>;
  execute(input: I, context: ToolContext): ToolOutcome | Promise<ToolOutcome>;
}

/** A command, with its handler. */
export interface CommandDefinition extends Omit<CommandRegisterParams, "id"> {
  id: string;
  /** Runs the command with the literal text after it, or the empty string. */
  execute(args: string, context: RequestContext): void | Promise<void>;
}

/** An interceptor's answer: run the call, or block it with a message for the model. */
export type Decision = "allow" | { reject: string };

/** Decides whether a tool call may run. */
export type Interceptor = (call: ToolCall, context: RequestContext) => Decision | Promise<Decision>;

/** What footer items show: text, styled segments, or a list of either. */
export type FooterContent = string | FooterSegment | readonly (string | FooterSegment)[];

/** A footer item the plugin set. */
export interface FooterItem {
  /** The canonical identifier, prefixed with the plugin's ID. */
  readonly id: string;
  readonly localId: string;
  /** Replaces the item's content, keeping its position. */
  set(content: FooterContent, request?: RequestOptions): Promise<void>;
  clear(request?: RequestOptions): Promise<void>;
}

/** A notification shown in the session's clients. */
export interface Toast {
  title: string;
  subtitle?: string | null;
  /** Defaults to info. */
  variant?: ToastVariant;
  persistent?: boolean;
}

/** Events Kit sends, with what listeners receive. */
export interface KitEvents {
  "session.changed": Session;
  "project.changed": Project;
  "git.changed": GitChangedParams;
  "turn.started": TurnStartedParams;
  "turn.completed": TurnCompletedParams;
}

/** The session the plugin runs in. */
export interface SessionAPI extends Readonly<Session> {
  /**
   * Starts a turn in this session with a message attributed to the plugin.
   * Rejects with a KitError whose reason is `session_busy` when the session
   * cannot start a turn now; Kit does not queue the message.
   */
  submitMessage(text: string, options?: RequestOptions & { idempotencyKey?: string }): Promise<SubmitMessageResult>;
}

/** What a plugin can do, grouped by kind. */
export interface Kit {
  /** The Kit server's version. */
  readonly version: string;
  /** This generation of the plugin; clients see the same identifier. */
  readonly instanceId: string;
  /** The session, kept current. */
  readonly session: SessionAPI;
  /** The session's working directory and Git state, kept current. */
  readonly project: Readonly<Project>;
  readonly commands: {
    register(command: CommandDefinition, request?: RequestOptions): Promise<Handle>;
  };
  readonly tools: {
    register<I>(tool: ToolDefinition<I>, request?: RequestOptions): Promise<ToolHandle>;
  };
  readonly interceptor: {
    /** Consults interceptor before every tool call. A plugin has one interceptor; registering replaces it. */
    register(interceptor: Interceptor, request?: RequestOptions): Promise<Omit<Handle, "id" | "localId">>;
  };
  readonly subagents: {
    register(subagent: SubagentRegisterParams, request?: RequestOptions): Promise<Handle>;
  };
  readonly footer: {
    set(id: string, content: FooterContent, request?: RequestOptions): Promise<FooterItem>;
    clear(id: string, request?: RequestOptions): Promise<void>;
    /** Hides an item, such as `kit.footer.location`, while the plugin claims it. */
    hide(target: string, request?: RequestOptions): Promise<void>;
    /** Withdraws the plugin's claim to hide an item. */
    show(target: string, request?: RequestOptions): Promise<void>;
  };
  /** Dialogs that belong to no request, and notifications. */
  readonly ui: Dialogs & { toast(toast: Toast): void };
  readonly system: {
    /** Asks the session's attached clients to open a web page. */
    openUrl(url: string, request?: RequestOptions): Promise<void>;
  };
  /** Listens to an event. Returns a function that stops listening. */
  on<E extends keyof KitEvents>(event: E, listener: (event: KitEvents[E]) => void | Promise<void>): () => void;
}

// ---------------------------------------------------------------------------
// The connection

/** How a connection reaches Kit. runPlugin uses stdio; tests use memory. */
export interface PluginIO {
  /** Writes one protocol line, including its newline. */
  write(line: string): void;
  /** Writes diagnostics. */
  log(text: string): void;
  /** Ends the plugin once written lines are delivered. */
  exit(code: number): void;
}

/** A plugin connected to Kit through a PluginIO. */
export interface Connection {
  /** Handles one line from Kit. */
  receive(line: string): void;
  /** Kit closed the connection: run the disposer and exit. */
  close(): void;
  /** Reports an error the plugin did not handle: diagnostics and an error toast. */
  report(error: unknown): void;
  /** Resolves when setup has finished, whether or not it succeeded. */
  readonly ready: Promise<void>;
  /** Resolves when no event listener is running. */
  idle(): Promise<void>;
}

type Pending = { resolve(value: unknown): void; reject(error: unknown): void };
type RpcMessage = {
  jsonrpc?: unknown;
  id?: RequestID | null;
  method?: unknown;
  params?: unknown;
  result?: unknown;
  error?: { code?: unknown; message?: unknown; data?: JsonValue };
};

const events: Record<string, keyof KitEvents> = {
  "kit/events/session.changed": "session.changed",
  "kit/events/project.changed": "project.changed",
  "kit/events/git.changed": "git.changed",
  "kit/events/agent.turn.started": "turn.started",
  "kit/events/agent.turn.completed": "turn.completed",
};

function describe(error: unknown): string {
  if (error instanceof Error) return error.stack ?? `${error.name}: ${error.message}`;
  return String(error);
}

function segments(content: FooterContent): FooterSegment[] {
  const list = typeof content === "string" || !Array.isArray(content) ? [content as string | FooterSegment] : content;
  return list.map((part) => (typeof part === "string" ? { text: part } : part));
}

/**
 * Connects setup to Kit through io. runPlugin calls it with stdio; the
 * testing helper calls it with an in-memory Kit.
 */
export function connect(setup: Setup, io: PluginIO): Connection {
  let initialized = false;
  let stopping: Promise<void> | undefined;
  let dispose: Disposer | undefined;
  let info: InitializeParams | undefined;
  let session: Session = { id: "", name: null };
  let project: Project = { cwd: "", git: null };
  let nextId = 0;
  let markReady: () => void = () => {};
  const ready = new Promise<void>((resolve) => {
    markReady = resolve;
  });
  let setupDone: Promise<void> = Promise.resolve();
  // Event listeners in progress, which idle() waits for.
  const working = new Set<Promise<unknown>>();
  const track = <T>(work: Promise<T>): Promise<T> => {
    working.add(work);
    void work.then(
      () => working.delete(work),
      () => working.delete(work),
    );
    return work;
  };

  const pending = new Map<string, Pending>();
  const active = new Map<string, AbortController>();
  const commands = new Map<string, CommandDefinition>();
  const tools = new Map<string, ToolDefinition<unknown>>();
  let interceptor: Interceptor | undefined;
  const listeners = new Map<keyof KitEvents, Set<(event: unknown) => void | Promise<void>>>();

  const keyOf = (id: unknown) => JSON.stringify(id);
  const send = (message: object) => io.write(`${JSON.stringify({ jsonrpc: "2.0", ...message })}\n`);

  function request<M extends keyof PluginRequests>(
    method: M,
    params: PluginRequests[M]["params"],
    options?: RequestOptions,
  ): Promise<PluginRequests[M]["result"]> {
    if (!initialized) return Promise.reject(new Error(`${method} sent before Kit initialized the plugin`));
    if (stopping) return Promise.reject(new KitError(ErrorCode.Unavailable, "The plugin is stopping"));
    const signal = options?.signal;
    if (signal?.aborted) return Promise.reject(signal.reason);
    const id = `plugin-${++nextId}`;
    return new Promise((resolve, reject) => {
      const onAbort = () => {
        if (!pending.delete(keyOf(id))) return;
        send({ method: "kit/cancel", params: { id } });
        reject(signal?.reason);
      };
      pending.set(keyOf(id), {
        resolve: (value) => {
          signal?.removeEventListener("abort", onAbort);
          resolve(value as PluginRequests[M]["result"]);
        },
        reject: (error) => {
          signal?.removeEventListener("abort", onAbort);
          reject(error);
        },
      });
      signal?.addEventListener("abort", onAbort, { once: true });
      send(params === undefined ? { id, method } : { id, method, params });
    });
  }

  function notify<M extends keyof PluginNotifications>(method: M, params: PluginNotifications[M]): void {
    if (initialized) send({ method, params });
  }

  function dialogs(parentId?: RequestID): Dialogs {
    const parent = parentId === undefined ? {} : { parentId };
    return {
      confirm: (options, signal) => request("kit/ui/confirm", { ...options, ...parent }, signal),
      input: (options, signal) => request("kit/ui/input", { ...options, ...parent }, signal),
      select: async (options, signal) => {
        const answer = await request("kit/ui/select", { ...options, options: [...options.options], ...parent }, signal);
        return answer as { value: never } | null;
      },
    };
  }

  function report(error: unknown): void {
    io.log(`${describe(error)}\n`);
    const message = error instanceof Error ? error.message : String(error);
    notify("kit/ui/toast", { title: "Plugin error", subtitle: message.slice(0, 4096), variant: "error" });
  }

  const kit: Kit = {
    get version() {
      return info?.kit.version ?? "";
    },
    get instanceId() {
      return info?.instance.id ?? "";
    },
    session: {
      get id() {
        return session.id;
      },
      get name() {
        return session.name;
      },
      submitMessage: (text, options) =>
        request(
          "kit/session/submit-message",
          options?.idempotencyKey === undefined ? { text } : { text, idempotencyKey: options.idempotencyKey },
          options,
        ),
    },
    get project() {
      return project;
    },
    commands: {
      async register(command, options) {
        const { execute: _, ...params } = command;
        const restore = install(commands, command.id, command);
        try {
          const { id } = await request("kit/commands/register", params, options);
          return handle(id, command.id, "kit/commands/unregister", () => {
            if (commands.get(command.id) === command) commands.delete(command.id);
          });
        } catch (error) {
          restore();
          throw error;
        }
      },
    },
    tools: {
      async register(tool, options) {
        const { execute: _, input, ...rest } = tool;
        const definition = tool as ToolDefinition<unknown>;
        const restore = install(tools, tool.id, definition);
        try {
          const result = await request(
            "kit/tools/register",
            { ...rest, inputSchema: input.schema as ToolInputSchema },
            options,
          );
          const removed = handle(result.id, tool.id, "kit/tools/unregister", () => {
            if (tools.get(tool.id) === definition) tools.delete(tool.id);
          });
          return { ...removed, modelName: result.modelName };
        } catch (error) {
          restore();
          throw error;
        }
      },
    },
    interceptor: {
      async register(next, options) {
        const previous = interceptor;
        interceptor = next;
        try {
          await request("kit/tool-calls/register-interceptor", undefined, options);
        } catch (error) {
          if (interceptor === next) interceptor = previous;
          throw error;
        }
        return {
          async unregister(request_?: RequestOptions) {
            if (interceptor !== next) return;
            await request("kit/tool-calls/unregister-interceptor", undefined, request_);
            if (interceptor === next) interceptor = undefined;
          },
        };
      },
    },
    subagents: {
      async register(subagent, options) {
        const { id } = await request("kit/subagents/register", subagent, options);
        return handle(id, subagent.id, "kit/subagents/unregister");
      },
    },
    footer: {
      async set(localId, content, options) {
        const { id } = await request("kit/footer/set", { id: localId, content: segments(content) }, options);
        return {
          id,
          localId,
          set: async (next, again) => {
            await request("kit/footer/set", { id: localId, content: segments(next) }, again);
          },
          clear: (again) => request("kit/footer/clear", { id: localId }, again).then(() => {}),
        };
      },
      clear: (id, options) => request("kit/footer/clear", { id }, options).then(() => {}),
      hide: (target, options) => request("kit/footer/hide", { id: target }, options).then(() => {}),
      show: (target, options) => request("kit/footer/show", { id: target }, options).then(() => {}),
    },
    ui: {
      ...dialogs(),
      toast: (toast) => notify("kit/ui/toast", { variant: "info", ...toast }),
    },
    system: {
      openUrl: (url, options) => request("kit/system/open-url", { url }, options).then(() => {}),
    },
    on(event, listener) {
      const set = listeners.get(event) ?? new Set();
      listeners.set(event, set);
      const entry = listener as (event: unknown) => void | Promise<void>;
      set.add(entry);
      return () => {
        set.delete(entry);
      };
    },
  };

  /**
   * Installs a handler before Kit can call it, and returns a function that
   * puts back what it replaced if Kit rejects the registration.
   */
  function install<V>(handlers: Map<string, V>, id: string, value: V): () => void {
    const previous = handlers.get(id);
    handlers.set(id, value);
    return () => {
      if (handlers.get(id) !== value) return;
      if (previous === undefined) handlers.delete(id);
      else handlers.set(id, previous);
    };
  }

  function handle(
    id: string,
    localId: string,
    method: "kit/commands/unregister" | "kit/tools/unregister" | "kit/subagents/unregister",
    removed?: () => void,
  ): Handle {
    return {
      id,
      localId,
      async unregister(options) {
        await request(method, { id: localId }, options);
        removed?.();
      },
    };
  }

  async function runSetup(): Promise<void> {
    try {
      const disposer = await setup(kit);
      if (typeof disposer === "function") dispose = disposer;
    } catch (error) {
      // A setup cut short by stopping is not a failure; stop exits.
      if (!stopping) {
        io.log(`Plugin setup failed: ${describe(error)}\n`);
        io.exit(1);
      }
    } finally {
      markReady();
    }
  }

  /**
   * Stops the plugin: cancels work in both directions, gives setup a moment
   * to finish, and runs its disposer once, even when setup returns it after
   * stopping began.
   */
  function stop(): Promise<void> {
    stopping ??= (async () => {
      for (const controller of active.values()) controller.abort(new KitError(ErrorCode.Cancelled, "The plugin is stopping"));
      for (const [id, waiting] of pending) {
        pending.delete(id);
        waiting.reject(new KitError(ErrorCode.Unavailable, "The plugin is stopping"));
      }
      // Kit stops waiting two seconds after shutdown, so give a setup that
      // is still running less than that to return its disposer.
      let timer: ReturnType<typeof setTimeout> | undefined;
      await Promise.race([setupDone, new Promise<void>((resolve) => (timer = setTimeout(resolve, SETUP_STOP_GRACE_MS)))]);
      clearTimeout(timer);
      try {
        await dispose?.();
      } catch (error) {
        io.log(`Plugin disposer failed: ${describe(error)}\n`);
      }
    })();
    return stopping;
  }

  async function serve(id: RequestID, method: string, params: unknown): Promise<unknown> {
    const context = (controller: AbortController): RequestContext => ({ signal: controller.signal, ui: dialogs(id) });
    const controller = new AbortController();
    switch (method) {
      case "kit/commands/execute": {
        const { id: local, args } = params as CommandExecuteParams;
        const command = commands.get(local);
        if (!command) throw new KitError(ErrorCode.Failed, `Unknown command: ${local}`);
        active.set(keyOf(id), controller);
        await command.execute(args, context(controller));
        return null;
      }
      case "kit/tools/execute": {
        const { id: local, toolCallId, input } = params as ToolExecuteParams;
        const tool = tools.get(local);
        if (!tool) throw new KitError(ErrorCode.Failed, `Unknown tool: ${local}`);
        active.set(keyOf(id), controller);
        const outcome = await tool.execute(input, { ...context(controller), toolCallId });
        return typeof outcome === "string" ? { content: [{ type: "text", text: outcome }] } : outcome;
      }
      case "kit/tool-calls/before-execute": {
        const current = interceptor;
        if (!current) throw new KitError(ErrorCode.Failed, "The plugin has no interceptor");
        active.set(keyOf(id), controller);
        const decision = await current((params as BeforeExecuteParams).toolCall, context(controller));
        if (decision === "allow") return { action: "allow" };
        return { action: "reject-and-continue", message: decision.reject };
      }
    }
    throw new Error(`Unexpected method ${method}`);
  }

  const served = new Set(["kit/commands/execute", "kit/tools/execute", "kit/tool-calls/before-execute"]);

  async function answer(id: RequestID, method: string, params: unknown): Promise<void> {
    if (method === "initialize") {
      const init = params as InitializeParams;
      if (initialized || init?.protocolVersion !== PROTOCOL_VERSION) {
        send({ id, error: { code: ErrorCode.Failed, message: `Unsupported protocol version ${init?.protocolVersion}` } });
        return;
      }
      info = init;
      session = init.context.session;
      project = init.context.project;
      initialized = true;
      send({ id, result: { protocolVersion: PROTOCOL_VERSION } });
      setupDone = runSetup();
      await setupDone;
      return;
    }
    if (method === "shutdown") {
      await stop();
      send({ id, result: null });
      io.exit(0);
      return;
    }
    if (!served.has(method)) {
      send({ id, error: { code: ErrorCode.MethodNotFound, message: `Method not found: ${method}` } });
      return;
    }
    try {
      const result = await serve(id, method, params);
      const cancelled = active.get(keyOf(id))?.signal.aborted;
      send(cancelled ? { id, error: { code: ErrorCode.Cancelled, message: "Cancelled" } } : { id, result });
    } catch (error) {
      // Kit's requests fail only as failed or cancelled; a KitError a handler
      // rethrows from Kit becomes a failure with its message.
      const cancelled = active.get(keyOf(id))?.signal.aborted;
      const code = cancelled ? ErrorCode.Cancelled : ErrorCode.Failed;
      const message = error instanceof Error ? error.message : String(error);
      send({ id, error: { code, message: message || "Failed" } });
    } finally {
      active.delete(keyOf(id));
    }
  }

  function observe(method: string, params: unknown): void {
    if (method === "kit/cancel") {
      active.get(keyOf((params as CancelParams)?.id))?.abort(new KitError(ErrorCode.Cancelled, "Kit cancelled the request"));
      return;
    }
    const event = events[method];
    if (!event) return; // Notifications this version does not know.
    if (event === "session.changed") session = params as Session;
    if (event === "project.changed") project = params as Project;
    if (event === "git.changed") project = { ...project, git: (params as GitChangedParams).git };
    for (const listener of [...(listeners.get(event) ?? [])]) {
      void track(
        Promise.resolve()
          .then(() => listener(params))
          .catch(report),
      );
    }
  }

  function receive(line: string): void {
    if (!line.trim()) return;
    let message: RpcMessage;
    try {
      message = JSON.parse(line);
    } catch {
      io.log(`Ignoring a line from Kit that is not JSON: ${line.slice(0, 200)}\n`);
      return;
    }
    if (!message || typeof message !== "object" || Array.isArray(message)) return;
    if (typeof message.method === "string") {
      if (message.id === undefined || message.id === null) observe(message.method, message.params);
      else void answer(message.id, message.method, message.params);
      return;
    }
    const waiting = pending.get(keyOf(message.id));
    if (!waiting) return;
    pending.delete(keyOf(message.id));
    if (message.error) {
      const { code, message: text, data } = message.error;
      waiting.reject(new KitError(typeof code === "number" ? code : ErrorCode.InternalError, String(text ?? "Kit request failed"), data));
    } else {
      waiting.resolve(message.result);
    }
  }

  return {
    receive,
    close() {
      void stop().then(() => io.exit(0));
    },
    report,
    ready,
    async idle() {
      while (working.size > 0) await Promise.allSettled([...working]);
    },
  };
}

/**
 * Runs a plugin over stdio. Writes to stdout and console output go to stderr,
 * so only protocol messages reach Kit. The plugin exits after shutdown, when
 * Kit closes stdin, or when setup fails.
 */
export function runPlugin(setup: Setup): void {
  // Node's process is the only global the helper needs; reach it without
  // requiring Node's type declarations.
  const host = (globalThis as unknown as { process: NodeLikeProcess }).process;
  const protocol = host.stdout.write.bind(host.stdout);
  const diagnostics = host.stderr.write.bind(host.stderr);
  host.stdout.write = ((chunk: unknown, ...rest: unknown[]) =>
    (diagnostics as (...args: unknown[]) => boolean)(chunk, ...rest)) as typeof host.stdout.write;
  for (const name of ["log", "info", "debug", "dir", "table"] as const) {
    console[name] = (...args: unknown[]) => console.error(...args);
  }

  const connection = connect(setup, {
    write: (line) => {
      protocol(line);
    },
    log: (text) => {
      diagnostics(text);
    },
    exit: (code) => {
      protocol("", () => host.exit(code));
    },
  });
  host.on("unhandledRejection", (reason: unknown) => connection.report(reason));

  let buffered = "";
  host.stdin.setEncoding("utf8");
  host.stdin.on("data", (chunk: string) => {
    buffered += chunk;
    for (let newline = buffered.indexOf("\n"); newline >= 0; newline = buffered.indexOf("\n")) {
      const line = buffered.slice(0, newline).replace(/\r$/, "");
      buffered = buffered.slice(newline + 1);
      connection.receive(line);
    }
  });
  host.stdin.on("end", () => connection.close());
}

interface NodeLikeStream {
  write(chunk: string, callback?: () => void): boolean;
}
interface NodeLikeProcess {
  stdout: NodeLikeStream;
  stderr: NodeLikeStream;
  stdin: {
    setEncoding(encoding: "utf8"): void;
    on(event: "data", listener: (chunk: string) => void): void;
    on(event: "end", listener: () => void): void;
  };
  on(event: "unhandledRejection", listener: (reason: unknown) => void): void;
  exit(code: number): never;
}

// ---------------------------------------------------------------------------
// Protocol types

// BEGIN GENERATED plugin protocol 2 types: go run ./api/pluginprotocol/cmd/emit
/** Any JSON value. */
export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue };

export interface BeforeExecuteParams {
  toolCall: ToolCall;
}

export interface CancelParams {
  id: RequestID;
}

export type CanonicalID = string;

export interface CanonicalIDResult {
  id: CanonicalID;
}

export interface CommandExecuteParams {
  /** The literal text after the command, or empty. */
  args: string;
  id: LocalID;
}

export interface CommandRegisterParams {
  /** Hint for the command's argument, at most 128 bytes. */
  argName?: string | null;
  /** Single-line, at most 128 bytes. */
  category?: string | null;
  /** Single-line, at most 1,024 bytes. */
  description: string;
  id: LocalID;
}

export interface ConfirmParams {
  /** At most 128 bytes. */
  cancelLabel?: string | null;
  /** At most 128 bytes. */
  confirmLabel?: string | null;
  /** The answer that has focus initially. */
  defaultValue?: boolean;
  /** Markdown, at most 8 KiB. */
  message?: string | null;
  /** A pending Kit request this dialog serves. Kit cancels the dialog when that request ends. */
  parentId?: RequestID;
  /** Single-line, at most 512 bytes. */
  title: string;
}

export type ConfirmResult = boolean;

export interface Context {
  project: Project;
  session: Session;
}

export interface ErrorData {
  reason?: ErrorReason;
}

export interface ErrorObject {
  code: number;
  /** Structured details. Errors from Kit use ErrorData. */
  data?: JsonValue;
  /** Concise and safe to display. */
  message: string;
}

export type ErrorReason = "busy" | "session_busy" | "rate_limited" | "interactivity_unavailable";

export type ExecutionMode = "sequential" | "parallel";

export interface FooterSegment {
  style?: FooterStyle;
  /** Single-line. An item's segments hold at most 4 KiB of text in total. */
  text: string;
}

export interface FooterSetParams {
  content: FooterSegment[];
  id: LocalID;
}

export interface FooterStyle {
  bg?: ThemeToken;
  bold?: boolean;
  dim?: boolean;
  fg?: ThemeToken;
  italic?: boolean;
  strikethrough?: boolean;
  underline?: boolean;
}

export type FooterTarget = string;

export interface FooterTargetParams {
  id: FooterTarget;
}

export interface Git {
  /** Null for a detached head. */
  branch: string | null;
  dirty: boolean;
  /** The branch's pull request; null when there is none or it is not known yet. */
  pullRequest: PullRequest | null;
  /** Absolute worktree root. */
  root: string;
}

export interface GitChangedParams {
  git: Git | null;
}

export type IdempotencyKey = string;

export interface InitializeParams {
  context: Context;
  instance: InstanceInfo;
  kit: KitInfo;
  protocolVersion: 2;
}

export interface InitializeResult {
  protocolVersion: 2;
}

export interface InputParams {
  /** At most 16 KiB. */
  initialValue?: string | null;
  /** Markdown, at most 8 KiB. */
  message?: string | null;
  /** A pending Kit request this dialog serves. Kit cancels the dialog when that request ends. */
  parentId?: RequestID;
  /** Single-line, at most 512 bytes. */
  placeholder?: string | null;
  /** Single-line, at most 512 bytes. */
  title: string;
}

export type InputResult = string | null;

export interface InstanceInfo {
  /** Opaque identifier of this generation. Clients see the same identifier on the plugin's toasts, dialogs, footer items, and commands. A replacement generation has a new one. */
  id: string;
}

export interface KitInfo {
  /** Kit server version, for diagnostics. */
  version: string;
}

export type LocalID = string;

export interface LocalIDParams {
  id: LocalID;
}

export interface Message {
  content: TextContent[];
  role: MessageRole;
}

export type MessageRole = "user" | "assistant";

export type ModelToolName = string;

export type Null = null;

export interface OpenURLParams {
  /** An absolute http or https URL without credentials. */
  url: string;
}

export interface Project {
  /** Absolute working directory. */
  cwd: string;
  /** Null outside a Git worktree or when Git cannot be inspected safely. */
  git: Git | null;
}

export interface PullRequest {
  /** Summary of the pull request's checks. */
  checks?: PullRequestChecks;
  number: number;
  url: string;
}

export interface PullRequestCheck {
  name: string;
  required: boolean;
  state: PullRequestCheckState;
  workflow?: string;
}

export interface PullRequestCheckCounts {
  canceled: number;
  failed: number;
  passed: number;
  pending: number;
  skipped: number;
  unknown: number;
}

export type PullRequestCheckState = "passed" | "failed" | "pending" | "skipped" | "canceled" | "unknown";

export interface PullRequestChecks {
  counts: PullRequestCheckCounts;
  items: PullRequestCheck[];
  omitted: number;
  required: PullRequestRequirement;
  state: PullRequestChecksState;
}

export type PullRequestChecksState = "passing" | "failing" | "pending" | "none";

export type PullRequestRequirement = "unknown" | "pending" | "satisfied" | "unsatisfied";

export type RequestID = string | number;

export interface SelectOption {
  /** At most 8 KiB. */
  description?: string | null;
  /** Single-line, at most 512 bytes. */
  label: string;
  /** Any JSON, at most 16 KiB; null is a valid value. */
  value: JsonValue;
}

export interface SelectParams {
  /** Markdown, at most 8 KiB. */
  message?: string | null;
  options: SelectOption[];
  /** A pending Kit request this dialog serves. Kit cancels the dialog when that request ends. */
  parentId?: RequestID;
  /** Single-line, at most 512 bytes. */
  placeholder?: string | null;
  /** Single-line, at most 512 bytes. */
  title: string;
}

export type SelectResult = {
  value: JsonValue;
} | null;

export interface Session {
  id: string;
  name: string | null;
}

export interface SubagentRegisterParams {
  /** Single-line, at most 1,024 bytes. */
  description: string;
  id: LocalID;
  /** The subagent's instructions, at most 128 KiB. */
  instructions: string;
  /** Kit model selector; defaults to the session's model. */
  model?: string | null;
}

export interface SubmitMessageParams {
  /** Repeating a key with the same text returns the original result. */
  idempotencyKey?: IdempotencyKey | null;
  /** At most 128 KiB; must not be blank or contain NUL. */
  text: string;
}

export interface SubmitMessageResult {
  messageId: string;
  turnId: string;
}

export interface TextContent {
  text: string;
  type: "text";
}

export type ThemeToken = "bg" | "bgSurface" | "bgMuted" | "bgAccent" | "bgTransparent" | "borderDefault" | "borderFocused" | "borderAccent" | "borderDebug" | "borderStatus" | "composerBashBorder" | "composerBashExcludedBorder" | "textPrimary" | "textSecondary" | "textMuted" | "textPlaceholder" | "textDebug" | "userText" | "userTextFocused" | "userBorder" | "assistantText" | "toolText" | "reviewText" | "errorText" | "warningText" | "subagentText" | "debugLabel" | "metaText" | "attachmentText" | "cursor" | "pickerBg" | "pickerBorder" | "pickerFocusedBg" | "pickerFocusedText" | "pickerItemText" | "pickerScrollThumb" | "pickerScrollTrack" | "scrollbarFg" | "scrollbarBg" | "panelText" | "progressNormal" | "progressWarning" | "progressCritical" | "toggleOn" | "diffAddedBg" | "diffRemovedBg" | "diffAddedContentBg" | "diffRemovedContentBg" | "diffAddedLineNumberBg" | "diffRemovedLineNumberBg" | "diffCursorBg" | "diffCursorGutterBg" | "diffCursorAddedBg" | "diffCursorRemovedBg";

export interface ToastParams {
  /** Keep the notification until the user dismisses it. */
  persistent?: boolean;
  /** At most 4,096 bytes; may contain newlines. */
  subtitle?: string | null;
  /** Single-line, at most 1,024 bytes. */
  title: string;
  variant: ToastVariant;
}

export type ToastVariant = "info" | "warning" | "error";

export interface ToolCall {
  id: string;
  input: { [key: string]: JsonValue };
  /** The model-facing tool name, such as bash or plugin__tool. */
  name: string;
}

export type ToolCallDecision = {
  action: "allow";
} | {
  action: "reject-and-continue";
  /** Shown to the model as the call's result. At most 8 KiB. */
  message: string;
};

export type ToolContent = {
  text: string;
  type: "text";
} | {
  data: string;
  mimeType: "image/png" | "image/jpeg" | "image/gif" | "image/webp";
  type: "image";
};

export interface ToolExecuteParams {
  id: ToolID;
  input: { [key: string]: JsonValue };
  toolCallId: string;
}

export interface ToolExecuteResult {
  content: ToolContent[];
  /** Any JSON, at most 64 KiB, kept with the call but not shown to the model. */
  details?: JsonValue;
  /** End the turn after this tool's batch completes. */
  terminate?: boolean;
}

export type ToolID = string;

export interface ToolIDParams {
  id: ToolID;
}

/** Kit's subset of JSON Schema draft 2020-12. Schemas cannot reference other schemas. At most 1,024 nodes and 16 levels. */
export interface ToolInputSchema {
  $schema?: "https://json-schema.org/draft/2020-12/schema";
  additionalProperties?: boolean;
  anyOf?: ToolSchemaNode[];
  const?: JsonValue;
  description?: string;
  enum?: JsonValue[];
  exclusiveMaximum?: number;
  exclusiveMinimum?: number;
  items?: ToolSchemaNode;
  maxItems?: number;
  maxLength?: number;
  maximum?: number;
  minItems?: number;
  minLength?: number;
  minimum?: number;
  pattern?: string;
  properties?: { [key: string]: ToolSchemaNode };
  required?: string[];
  type: "object";
}

export interface ToolRegisterParams {
  /** Model-facing description, at most 8 KiB. */
  description: string;
  /** Defaults to sequential. */
  executionMode?: ExecutionMode;
  id: ToolID;
  inputSchema: ToolInputSchema;
  /** Single-line display label, at most 512 bytes. Defaults to the ID. */
  label?: string | null;
  /** Each at most 1 KiB. */
  promptGuidelines?: string[];
  /** At most 8 KiB. */
  promptSnippet?: string | null;
}

export interface ToolRegisterResult {
  id: CanonicalID;
  modelName: ModelToolName;
}

export interface ToolSchemaNode {
  $schema?: "https://json-schema.org/draft/2020-12/schema";
  additionalProperties?: boolean;
  anyOf?: ToolSchemaNode[];
  const?: JsonValue;
  description?: string;
  enum?: JsonValue[];
  exclusiveMaximum?: number;
  exclusiveMinimum?: number;
  items?: ToolSchemaNode;
  maxItems?: number;
  maxLength?: number;
  maximum?: number;
  minItems?: number;
  minLength?: number;
  minimum?: number;
  pattern?: string;
  properties?: { [key: string]: ToolSchemaNode };
  required?: string[];
  type?: "object" | "array" | "string" | "number" | "integer" | "boolean" | "null";
}

export interface Turn {
  id: string;
  messages: Message[];
}

export interface TurnCompletedParams {
  sessionId: string;
  turn: Turn;
}

export interface TurnStartedParams {
  sessionId: string;
  turnId: string;
}

/** Requests a plugin sends Kit, with their params and results. */
export interface PluginRequests {
  /** Registers a command. */
  "kit/commands/register": { params: CommandRegisterParams; result: CanonicalIDResult };
  /** Removes a command; removing an unknown command succeeds. */
  "kit/commands/unregister": { params: LocalIDParams; result: Null };
  /** Registers a tool the model may call. */
  "kit/tools/register": { params: ToolRegisterParams; result: ToolRegisterResult };
  /** Removes a tool; removing an unknown tool succeeds. */
  "kit/tools/unregister": { params: ToolIDParams; result: Null };
  /** Asks Kit to consult the plugin before every tool call. */
  "kit/tool-calls/register-interceptor": { params: undefined; result: Null };
  /** Stops consulting the plugin before tool calls. */
  "kit/tool-calls/unregister-interceptor": { params: undefined; result: Null };
  /** Registers a subagent definition. */
  "kit/subagents/register": { params: SubagentRegisterParams; result: CanonicalIDResult };
  /** Removes a subagent definition. */
  "kit/subagents/unregister": { params: LocalIDParams; result: Null };
  /** Creates or replaces a footer item. */
  "kit/footer/set": { params: FooterSetParams; result: CanonicalIDResult };
  /** Removes a footer item. */
  "kit/footer/clear": { params: LocalIDParams; result: Null };
  /** Hides a footer item while the claim lasts. */
  "kit/footer/hide": { params: FooterTargetParams; result: Null };
  /** Withdraws the plugin's hide claim. */
  "kit/footer/show": { params: FooterTargetParams; result: Null };
  /** Asks the user a yes-or-no question. */
  "kit/ui/confirm": { params: ConfirmParams; result: ConfirmResult };
  /** Asks the user for text. */
  "kit/ui/input": { params: InputParams; result: InputResult };
  /** Asks the user to choose an option. */
  "kit/ui/select": { params: SelectParams; result: SelectResult };
  /** Starts a turn in the plugin's idle session. */
  "kit/session/submit-message": { params: SubmitMessageParams; result: SubmitMessageResult };
  /** Asks the session's attached clients to open a web page. */
  "kit/system/open-url": { params: OpenURLParams; result: Null };
}

/** Notifications a plugin sends Kit, with their params. */
export interface PluginNotifications {
  /** Cancels a request the sender sent earlier. */
  "kit/cancel": CancelParams;
  /** Shows a notification in the session's clients. */
  "kit/ui/toast": ToastParams;
}

/** Requests Kit sends a plugin, with their params and results. */
export interface KitRequests {
  /** Opens the conversation; the plugin answers before sending anything else. */
  "initialize": { params: InitializeParams; result: InitializeResult };
  /** Asks the plugin to stop; it answers null and exits within two seconds. */
  "shutdown": { params: undefined; result: Null };
  /** Runs a command a user invoked. */
  "kit/commands/execute": { params: CommandExecuteParams; result: Null };
  /** Runs a tool the model called. */
  "kit/tools/execute": { params: ToolExecuteParams; result: ToolExecuteResult };
  /** Asks an interceptor whether a tool call may run. */
  "kit/tool-calls/before-execute": { params: BeforeExecuteParams; result: ToolCallDecision };
}

/** Notifications Kit sends a plugin, with their params. */
export interface KitNotifications {
  /** Cancels a request the sender sent earlier. */
  "kit/cancel": CancelParams;
  /** The session's working directory changed; sent to user plugins only. */
  "kit/events/project.changed": Project;
  /** The session was renamed. */
  "kit/events/session.changed": Session;
  /** The working directory's Git state or pull request changed. */
  "kit/events/git.changed": GitChangedParams;
  /** Kit admitted a turn. */
  "kit/events/agent.turn.started": TurnStartedParams;
  /** A turn settled; carries its user and assistant text. */
  "kit/events/agent.turn.completed": TurnCompletedParams;
}

/** Error codes, by name. */
export const ErrorCode = {
  /** The line is not valid JSON or UTF-8. */
  ParseError: -32700,
  /** The message is not a valid request, including a batch. */
  InvalidRequest: -32600,
  /** Unknown method. */
  MethodNotFound: -32601,
  /** Invalid params; the message names the rule broken. */
  InvalidParams: -32602,
  /** Internal error. */
  InternalError: -32603,
  /** A command or tool failed, or Kit cannot show dialogs (data.reason interactivity_unavailable). */
  Failed: -32000,
  /** The request was cancelled. */
  Cancelled: -32001,
  /** The instance's generation is stopping, stopped, or replaced. */
  Unavailable: -32002,
  /** A duplicate contribution, or an idempotency key reused with different text. */
  Conflict: -32003,
  /** A size or count limit was exceeded. */
  LimitExceeded: -32005,
  /** Try again later; data.reason says why. */
  TryLater: -32006,
} as const;
// END GENERATED plugin protocol 2 types
