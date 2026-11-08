// The session API over fetch: the bearer token, the server instance, and the
// protocol on every request, and the contract's error body decoded.
import {
  type AttachmentInfo,
  type AttachmentResolution,
  type BashAbortResult,
  type BashExecution,
  type BashExecutionInput,
  type CompactSessionResult,
  type ConfigureSessionResult,
  type CreateSessionInput,
  type DeviceInfo,
  type ForkSessionResult,
  type Health,
  type HighlightCodeBlock,
  type HighlightCodeResult,
  type InteractionResponse,
  type MessagePage,
  type ModelCapability,
  PROTOCOL_VERSION,
  type PromoteFollowUpsResult,
  type PromptInput,
  type PromptSubmission,
  type RedeemPairingResult,
  type RestoreFollowUpsResult,
  type ServerSettings,
  type SessionActivityEntry,
  type SessionInfo,
  type SessionList,
  type SessionSnapshot,
  type TranscriptPage,
} from "./types";

/** A declared failure from the session API, by its stable code. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    /** The error's declared details, such as a failed compaction's reason. */
    readonly details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = "ApiError";
  }

  /** Whether the server no longer accepts the device token. */
  get unauthorized(): boolean {
    return this.status === 401;
  }

  /** Whether the page belongs to another server instance or protocol. */
  get mismatch(): boolean {
    return this.code === "instance_mismatch" || this.code === "protocol_mismatch";
  }
}

/** The request never reached the server, or its answer never arrived. */
export class NetworkError extends Error {
  override name = "NetworkError";
}

type Fetch = (input: string, init?: RequestInit) => Promise<Response>;

interface RequestOptions {
  body?: unknown;
  /** A multipart body; the browser sets its content type and boundary. */
  form?: FormData;
  query?: Record<string, string | number | undefined>;
  signal?: AbortSignal;
  /** Session routes need the protocol header; device routes declare only the instance. */
  protocol?: boolean;
  /** Health is the one call made before the instance is known. */
  instance?: boolean;
}

export class Api {
  /** The server instance this page talks to, learned from health. */
  instanceId = "";

  constructor(
    private readonly token: string,
    fetcher?: Fetch,
  ) {
    // fetch must be called on the window, including when a page tool wraps it.
    this.fetcher = fetcher ?? ((input, init) => globalThis.fetch(input, init));
  }

  private readonly fetcher: Fetch;

  /** Learns the server's instance and checks its protocol. */
  async handshake(signal?: AbortSignal): Promise<Health> {
    const health = await this.request<Health>("GET", "/v1/health", {
      signal,
      instance: false,
      protocol: false,
    });
    if (typeof health.instanceId !== "string" || health.instanceId === "") {
      throw new ApiError(502, "invalid_response", "The server did not say which instance it is.");
    }
    this.instanceId = health.instanceId;
    return health;
  }

  /**
   * Lists the server's sessions. With `order` the server may return them in
   * that order with their activity; `SessionList.order` says whether it did.
   */
  listSessions(signal?: AbortSignal, order?: "attention"): Promise<SessionList> {
    return this.request("GET", "/v1/sessions", { signal, query: { order } });
  }

  /** Starts a shell command; its id, chosen by the client, makes a retry safe. */
  startBash(id: string, input: BashExecutionInput): Promise<BashExecution> {
    return this.request("POST", `/v1/sessions/${segment(id)}/bash-executions`, { body: input });
  }

  getBash(id: string, execution: string, signal?: AbortSignal): Promise<BashExecution> {
    return this.request("GET", `/v1/sessions/${segment(id)}/bash-executions/${segment(execution)}`, {
      signal,
    });
  }

  abortBash(id: string, execution: string): Promise<BashAbortResult> {
    return this.request("POST", `/v1/sessions/${segment(id)}/bash-executions/${segment(execution)}/abort`);
  }

  sessionActivity(signal?: AbortSignal): Promise<{ sessions: SessionActivityEntry[] }> {
    return this.request("GET", "/v1/sessions/activity", { signal });
  }

  createSession(input: CreateSessionInput): Promise<SessionInfo> {
    return this.request("POST", "/v1/sessions", { body: input });
  }

  renameSession(id: string, name: string): Promise<SessionInfo> {
    return this.request("PATCH", `/v1/sessions/${segment(id)}`, { body: { name } });
  }

  /** Deletes a saved session and its history. */
  deleteSession(id: string): Promise<void> {
    return this.request("DELETE", `/v1/sessions/${segment(id)}`);
  }

  /** Removes a temporary session from the server's memory. */
  disposeSession(id: string): Promise<void> {
    return this.request("POST", `/v1/sessions/${segment(id)}/dispose`);
  }

  forkSession(id: string, input: { name?: string; prompt?: PromptInput }): Promise<ForkSessionResult> {
    return this.request("POST", `/v1/sessions/${segment(id)}/forks`, { body: input });
  }

  /** Compacts the session's context; the operation id makes a retry resolve the same request. */
  compactSession(id: string, operationId: string): Promise<CompactSessionResult> {
    return this.request("POST", `/v1/sessions/${segment(id)}/compact`, { body: { operationId } });
  }

  serverSettings(signal?: AbortSignal): Promise<ServerSettings> {
    return this.request("GET", "/v1/settings", { signal, protocol: false });
  }

  getSession(id: string, signal?: AbortSignal): Promise<SessionSnapshot> {
    return this.request("GET", `/v1/sessions/${segment(id)}`, { signal });
  }

  transcriptPage(id: string, before: string, signal?: AbortSignal): Promise<TranscriptPage> {
    return this.request("GET", `/v1/sessions/${segment(id)}/transcript`, { signal, query: { before } });
  }

  /** Opens the session's event stream after a sequence. */
  streamEvents(id: string, stream: string, after: number, signal: AbortSignal): Promise<Response> {
    return this.send("GET", `/v1/sessions/${segment(id)}/events/stream`, {
      signal,
      query: { stream: stream || undefined, after },
    });
  }

  submitPrompt(id: string, input: PromptInput): Promise<PromptSubmission> {
    return this.request("POST", `/v1/sessions/${segment(id)}/turns/submissions`, { body: input });
  }

  /** Uploads one file to the session; the server checks its type and limits. */
  uploadAttachment(id: string, file: Blob, filename: string, signal?: AbortSignal): Promise<AttachmentInfo> {
    const form = new FormData();
    form.append("file", file, filename);
    return this.request("POST", `/v1/sessions/${segment(id)}/attachments`, { form, signal });
  }

  /** An attachment's bytes. */
  async readAttachment(id: string, attachment: string, signal?: AbortSignal): Promise<Blob> {
    const response = await this.send(
      "GET",
      `/v1/sessions/${segment(id)}/attachments/${segment(attachment)}`,
      { signal },
    );
    return response.blob();
  }

  /** Metadata for attachments by id; ids the session no longer has come back missing. */
  resolveAttachments(
    id: string,
    attachmentIds: string[],
    signal?: AbortSignal,
  ): Promise<AttachmentResolution> {
    return this.request("POST", `/v1/sessions/${segment(id)}/attachments/resolve`, {
      body: { attachmentIds },
      signal,
    });
  }

  /** The session's latest user messages, newest first. */
  recentUserMessages(id: string, limit: number, signal?: AbortSignal): Promise<MessagePage> {
    return this.request("GET", `/v1/sessions/${segment(id)}/messages`, {
      signal,
      query: { limit, role: "user" },
    });
  }

  abortTurn(id: string, turn: string): Promise<{ aborting: boolean }> {
    return this.request("POST", `/v1/sessions/${segment(id)}/turns/${segment(turn)}/abort`);
  }

  restoreFollowUps(id: string): Promise<RestoreFollowUpsResult> {
    return this.request("POST", `/v1/sessions/${segment(id)}/turns/follow-ups/restore`);
  }

  promoteFollowUps(id: string): Promise<PromoteFollowUpsResult> {
    return this.request("POST", `/v1/sessions/${segment(id)}/turns/follow-ups/promote`);
  }

  respondInteraction(id: string, interaction: string, response: InteractionResponse): Promise<unknown> {
    return this.request("POST", `/v1/sessions/${segment(id)}/interactions/${segment(interaction)}/response`, {
      body: response,
    });
  }

  configureSession(
    id: string,
    input: { expectedRevision: number; model: string; thinkingLevel?: string },
  ): Promise<ConfigureSessionResult> {
    return this.request("POST", `/v1/sessions/${segment(id)}/configure`, { body: input });
  }

  listModels(signal?: AbortSignal): Promise<{ models: ModelCapability[] }> {
    return this.request("GET", "/v1/models", { signal });
  }

  /** Colors code blocks in one batch; a block the server can't color comes back without `highlight`. */
  highlightCode(blocks: HighlightCodeBlock[], signal?: AbortSignal): Promise<HighlightCodeResult> {
    return this.request("POST", "/v1/highlight", { body: { blocks }, signal });
  }

  listDevices(signal?: AbortSignal): Promise<{ devices: DeviceInfo[] }> {
    return this.request("GET", "/v1/devices", { signal, protocol: false });
  }

  revokeDevice(id: string): Promise<void> {
    return this.request("DELETE", `/v1/devices/${segment(id)}`, { protocol: false });
  }

  /** Redeems a pairing code; needs no credential, instance, or protocol. */
  static async redeemPairing(
    code: string,
    deviceName: string,
    fetcher?: Fetch,
  ): Promise<RedeemPairingResult> {
    const api = new Api("", fetcher);
    return api.request("POST", "/v1/pairing/redeem", {
      body: { code, deviceName },
      instance: false,
      protocol: false,
    });
  }

  async request<T>(method: string, path: string, options: RequestOptions = {}): Promise<T> {
    const response = await this.send(method, path, options);
    if (response.status === 204) return undefined as T;
    try {
      return (await response.json()) as T;
    } catch {
      throw new ApiError(response.status, "invalid_response", "The server's answer could not be read.");
    }
  }

  /** Sends a request and returns its successful response, or throws its declared error. */
  async send(method: string, path: string, options: RequestOptions = {}): Promise<Response> {
    const headers: Record<string, string> = { Accept: "application/json" };
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    if (options.instance !== false && this.instanceId) headers["X-Kit-Instance-ID"] = this.instanceId;
    if (options.protocol !== false && options.instance !== false) {
      headers["X-Kit-Protocol-Version"] = String(PROTOCOL_VERSION);
    }
    let body: string | FormData | undefined = options.form;
    if (options.body !== undefined) {
      headers["Content-Type"] = "application/json";
      body = JSON.stringify(options.body);
    }
    let url = path;
    if (options.query) {
      const params = new URLSearchParams();
      for (const [key, value] of Object.entries(options.query)) {
        if (value !== undefined) params.set(key, String(value));
      }
      const query = params.toString();
      if (query) url += `?${query}`;
    }
    let response: Response;
    try {
      response = await this.fetcher(url, {
        method,
        headers,
        body,
        signal: options.signal,
        credentials: "omit",
        cache: "no-store",
      });
    } catch (error) {
      if (isAbort(error)) throw error;
      throw new NetworkError("Kit’s server can’t be reached.");
    }
    if (!response.ok) throw await decodeError(response);
    return response;
  }
}

/** Reads the contract's error body: `{"error": {"code", "message"}}`. */
export async function decodeError(response: Response): Promise<ApiError> {
  let code = `http_${response.status}`;
  let message = `The server answered ${response.status}.`;
  let details: Record<string, unknown> | undefined;
  try {
    const body = (await response.json()) as {
      error?: { code?: unknown; message?: unknown; details?: unknown };
    };
    if (typeof body.error?.code === "string") code = body.error.code;
    if (typeof body.error?.message === "string" && body.error.message) message = body.error.message;
    const found = body.error?.details;
    if (found && typeof found === "object" && !Array.isArray(found))
      details = found as Record<string, unknown>;
  } catch {
    // An undeclared body keeps the status.
  }
  return new ApiError(response.status, code, message, details);
}

export function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

/** One path segment, encoded. */
function segment(value: string): string {
  return encodeURIComponent(value);
}
