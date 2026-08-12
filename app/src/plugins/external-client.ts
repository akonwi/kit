import { type ChildProcessWithoutNullStreams, spawn } from "node:child_process";
import path from "node:path";
import type { TSchema } from "typebox";
import { Check } from "typebox/schema";
import type { Command } from "../features/commands/types";
import type { AgentTool } from "../runtime/agent";
import type { ToolApprovalRequest } from "../runtime/agent-runtime";
import type { Turn } from "../session/types";
import type {
	ChromeTextSegment,
	ChromeThemeToken,
} from "../shell/chrome-contributions";
import { openExternal } from "../shell/open-external";
import type { ExternalPluginFailure, ExternalPluginManifest } from "./external";
import {
	createSchemaValidator,
	JsonRpcEndpoint,
	JsonRpcError,
	type JsonValue,
} from "./json-rpc";
import type { PluginContext } from "./types";

type ClientState =
	| "created"
	| "initializing"
	| "ready"
	| "stopping"
	| "stopped";

type CommandRegisterParams = {
	id: string;
	description: string;
	argName?: string | null;
	category?: string | null;
};

type ToolRegisterParams = {
	id: string;
	label?: string | null;
	description: string;
	inputSchema: Record<string, JsonValue>;
	executionMode?: "parallel" | "sequential";
	promptSnippet?: string | null;
	promptGuidelines?: string[];
};

type ToolExecuteResult = {
	content: Array<
		| { type: "text"; text: string }
		| { type: "image"; data: string; mimeType: string }
	>;
	details?: JsonValue;
	terminate?: boolean;
};

type ChromeSetParams = {
	id: string;
	content: Array<{
		text: string;
		style?: {
			fg?: ChromeThemeToken;
			bg?: ChromeThemeToken;
			bold?: boolean;
			dim?: boolean;
			italic?: boolean;
			underline?: boolean;
			strikethrough?: boolean;
		};
	}>;
	side?: "left" | "right";
	clickable?: boolean;
};

type SubagentRegisterParams = {
	id: string;
	description: string;
	instructions: string;
	model?: string | null;
};

type SelectParams = {
	title: string;
	message?: string | null;
	options: Array<{
		label: string;
		value: JsonValue;
		description?: string | null;
	}>;
	filterable?: boolean;
	placeholder?: string | null;
};

type SystemPromptSlot = ReturnType<
	PluginContext["runtime"]["createSystemPromptSlot"]
>;

const INITIALIZE_TIMEOUT_MS = 10_000;
const SHUTDOWN_TIMEOUT_MS = 2_000;
const FORCE_KILL_DELAY_MS = 500;
const STDERR_TAIL_BYTES = 64 * 1024;
const SAFE_ERROR_LENGTH = 500;

const validateInitializeResult = createSchemaValidator("InitializeResult");
const validateNullResult = createSchemaValidator("NullResult");
const validateToolExecuteResult = createSchemaValidator("ToolExecuteResult");
const validateToolCallDecision = createSchemaValidator("ToolCallDecision");

function isRecord(value: unknown): value is Record<string, JsonValue> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

function sanitizeDiagnostic(value: string): string {
	return [...value]
		.map((character) => {
			const code = character.codePointAt(0) ?? 0;
			return code < 32 || code === 127 ? " " : character;
		})
		.join("");
}

function safeErrorMessage(error: unknown): string {
	return sanitizeDiagnostic(errorMessage(error)).slice(0, SAFE_ERROR_LENGTH);
}

function toCanonicalId(pluginId: string, localId: string): string {
	return `${pluginId}.${localId}`;
}

function toModelToolName(pluginId: string, localId: string): string {
	return `${pluginId}__${localId}`;
}

function resolveCommand(root: string, command: string): string {
	return command.includes("/") || command.includes("\\")
		? path.resolve(root, command)
		: command;
}

function delay(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForAbortable<T>(
	promise: Promise<T>,
	signal?: AbortSignal,
): Promise<T> {
	if (!signal) return promise;
	if (signal.aborted) throw new Error("Plugin initialization aborted");
	let rejectAbort: ((error: Error) => void) | null = null;
	const aborted = new Promise<never>((_resolve, reject) => {
		rejectAbort = reject;
	});
	const onAbort = () =>
		rejectAbort?.(new Error("Plugin initialization aborted"));
	signal.addEventListener("abort", onAbort, { once: true });
	try {
		return await Promise.race([promise, aborted]);
	} finally {
		signal.removeEventListener("abort", onAbort);
	}
}

function publicTurn(turn: Turn): JsonValue {
	const messages: JsonValue[] = [];
	for (const message of turn.messages) {
		if (message.role !== "user" && message.role !== "assistant") continue;
		const content: JsonValue[] = [];
		if (typeof message.content === "string") {
			if (message.content.length > 0) {
				content.push({ type: "text", text: message.content });
			}
		} else if (Array.isArray(message.content)) {
			for (const part of message.content) {
				if (
					isRecord(part) &&
					part.type === "text" &&
					typeof part.text === "string"
				) {
					content.push({ type: "text", text: part.text });
				}
			}
		}
		if (content.length > 0) messages.push({ role: message.role, content });
	}
	return { id: turn.id, messages };
}

function toChromeContent(params: ChromeSetParams): ChromeTextSegment[] {
	return params.content.map((segment) => ({
		text: segment.text,
		style: segment.style
			? {
					...(segment.style.fg ? { fgToken: segment.style.fg } : {}),
					...(segment.style.bg ? { bgToken: segment.style.bg } : {}),
					bold: segment.style.bold,
					dim: segment.style.dim,
					italic: segment.style.italic,
					underline: segment.style.underline,
					strikethrough: segment.style.strikethrough,
				}
			: undefined,
	}));
}

export class ExternalPluginClient {
	readonly manifest: ExternalPluginManifest;
	private readonly context: PluginContext;
	private readonly onFailure: (failure: ExternalPluginFailure) => void;
	private state: ClientState = "created";
	private process: ChildProcessWithoutNullStreams | null = null;
	private endpoint: JsonRpcEndpoint | null = null;
	private stderrTail = Buffer.alloc(0);
	private exitCode: number | null = null;
	private exitSignal: NodeJS.Signals | null = null;
	private exitPromise: Promise<void> = Promise.resolve();
	private resolveExit: (() => void) | null = null;
	private stopPromise: Promise<void> | null = null;
	private failureReported = false;
	private readonly commandDisposers = new Map<string, () => void>();
	private readonly toolDisposers = new Map<string, () => void>();
	private readonly subagentDisposers = new Map<string, () => void>();
	private readonly headerIds = new Set<string>();
	private readonly footerIds = new Set<string>();
	private readonly headerHideClaims = new Map<string, () => void>();
	private readonly footerHideClaims = new Map<string, () => void>();
	private interceptorDisposer: (() => void) | null = null;
	private systemPromptSlot: SystemPromptSlot | null = null;

	constructor(options: {
		manifest: ExternalPluginManifest;
		context: PluginContext;
		onFailure: (failure: ExternalPluginFailure) => void;
	}) {
		this.manifest = options.manifest;
		this.context = options.context;
		this.onFailure = options.onFailure;
	}

	get ready(): boolean {
		return this.state === "ready";
	}

	async start(signal?: AbortSignal): Promise<void> {
		if (this.state !== "created") return;
		this.state = "initializing";

		try {
			try {
				await this.launch();
			} catch (error) {
				this.reportFailure("launch", error);
				throw error;
			}
			const endpoint = this.requireEndpoint();
			const session = this.context.runtime.getSession();
			const initialize = endpoint.request(
				"initialize",
				{
					protocolVersion: 1,
					context: {
						project: {
							cwd: session.cwd,
							git: this.context.runtime.vcsInfo,
						},
						session: { id: session.id, name: session.name ?? null },
					},
				},
				{
					validateResult: validateInitializeResult,
					onResult: () => {
						if (this.state !== "initializing") return;
						this.systemPromptSlot =
							this.context.runtime.createSystemPromptSlot();
						this.state = "ready";
					},
				},
			);
			await waitForAbortable(
				Promise.race([
					initialize,
					delay(INITIALIZE_TIMEOUT_MS).then(() => {
						throw new Error("Initialization timed out after 10 seconds");
					}),
				]),
				signal,
			);
		} catch (error) {
			this.reportFailure("initialize", error);
			await this.stop(false);
			throw error;
		}
	}

	notify(method: string, params?: JsonValue): void {
		if (!this.ready || !this.endpoint) return;
		void this.endpoint.notify(method, params).catch(() => {});
	}

	stop(graceful = true, signal?: AbortSignal): Promise<void> {
		if (this.state === "stopped") return Promise.resolve();
		if (this.stopPromise) return this.stopPromise;
		this.stopPromise = this.performStop(graceful, signal);
		return this.stopPromise;
	}

	private async performStop(
		graceful: boolean,
		signal?: AbortSignal,
	): Promise<void> {
		this.state = "stopping";
		this.removeContributions();
		const proc = this.process;
		const endpoint = this.endpoint;
		if (!proc) {
			this.state = "stopped";
			return;
		}
		const abort = () => this.kill("SIGTERM");
		if (signal?.aborted) abort();
		else signal?.addEventListener("abort", abort, { once: true });
		try {
			if (
				graceful &&
				endpoint &&
				proc.exitCode == null &&
				proc.signalCode == null
			) {
				endpoint.cancelPendingRequests();
				void endpoint
					.request("shutdown", undefined, {
						validateResult: validateNullResult,
					})
					.catch(() => {});
				await Promise.race([this.exitPromise, delay(SHUTDOWN_TIMEOUT_MS)]);
			}

			if (proc.exitCode == null && proc.signalCode == null) {
				this.kill("SIGTERM");
				await Promise.race([this.exitPromise, delay(FORCE_KILL_DELAY_MS)]);
			}
			if (proc.exitCode == null && proc.signalCode == null)
				this.kill("SIGKILL");
			await Promise.race([this.exitPromise, delay(FORCE_KILL_DELAY_MS)]);
			endpoint?.close(new JsonRpcError(-32002, "Plugin stopped"));
			this.state = "stopped";
		} finally {
			signal?.removeEventListener("abort", abort);
		}
	}

	private async launch(): Promise<void> {
		const transport = this.manifest.manifest.transport;
		const command = resolveCommand(this.manifest.root, transport.command);
		const proc = spawn(command, transport.args ?? [], {
			cwd: this.manifest.root,
			env: process.env,
			detached: process.platform !== "win32",
			stdio: ["pipe", "pipe", "pipe"],
			windowsHide: true,
		});
		this.process = proc;
		this.exitPromise = new Promise((resolve) => {
			this.resolveExit = resolve;
		});
		proc.stderr.on("data", (chunk: Buffer) => this.appendStderr(chunk));
		proc.once("exit", (code, signal) => this.handleExit(code, signal));

		await new Promise<void>((resolve, reject) => {
			const onSpawn = () => {
				proc.off("error", onError);
				resolve();
			};
			const onError = (error: Error) => {
				proc.off("spawn", onSpawn);
				reject(error);
			};
			proc.once("spawn", onSpawn);
			proc.once("error", onError);
		});

		this.endpoint = new JsonRpcEndpoint({
			input: proc.stdout,
			output: proc.stdin,
			requestIdPrefix: "kit",
			handleRequest: (method, params, signal) =>
				this.handleRequest(method, params, signal),
			handleNotification: (method, params) =>
				this.handleNotification(method, params),
			onFatal: (error) => this.protocolFailure(error),
		});
	}

	private async handleRequest(
		method: string,
		params: JsonValue | undefined,
		signal: AbortSignal,
	): Promise<JsonValue> {
		this.requireReady();

		switch (method) {
			case "kit/commands/register":
				return this.registerCommand(params as CommandRegisterParams);
			case "kit/commands/unregister":
				return this.unregister(this.commandDisposers, params);
			case "kit/tools/register":
				return this.registerTool(params as ToolRegisterParams);
			case "kit/tools/unregister":
				return this.unregister(this.toolDisposers, params);
			case "kit/tool-calls/register-interceptor":
				return this.registerInterceptor();
			case "kit/tool-calls/unregister-interceptor":
				this.interceptorDisposer?.();
				this.interceptorDisposer = null;
				return null;
			case "kit/ui/confirm":
				return this.confirm(params, signal);
			case "kit/ui/input":
				return this.input(params, signal);
			case "kit/ui/select":
				return this.select(params as SelectParams, signal);
			case "kit/header/set":
				return this.setChrome("header", params as ChromeSetParams);
			case "kit/footer/set":
				return this.setChrome("footer", params as ChromeSetParams);
			case "kit/header/clear":
				return this.clearChrome("header", params);
			case "kit/footer/clear":
				return this.clearChrome("footer", params);
			case "kit/header/hide":
				return this.hideChrome("header", params);
			case "kit/footer/hide":
				return this.hideChrome("footer", params);
			case "kit/header/show":
				return this.showChrome("header", params);
			case "kit/footer/show":
				return this.showChrome("footer", params);
			case "kit/subagents/register":
				return this.registerSubagent(params as SubagentRegisterParams);
			case "kit/subagents/unregister":
				return this.unregister(this.subagentDisposers, params);
			case "kit/system-prompt/set":
				this.systemPromptSlot?.set(
					(params as unknown as { text: string }).text,
				);
				return null;
			case "kit/system-prompt/clear":
				this.systemPromptSlot?.clear();
				return null;
			case "kit/session/submit-message":
				return this.submitMessage(params);
			case "kit/system/open-url":
				await openExternal((params as unknown as { url: string }).url);
				return null;
			default:
				throw new JsonRpcError(-32601, `Method not found: ${method}`);
		}
	}

	private handleNotification(
		method: string,
		params: JsonValue | undefined,
	): void {
		if (this.state === "stopping" || this.state === "stopped") return;
		this.requireReady();
		if (method !== "kit/ui/toast") return;
		const toast = params as unknown as {
			title: string;
			subtitle?: string | null;
			variant: "error" | "info" | "warning";
			persistent?: boolean;
		};
		this.context.ui.toast({
			title: toast.title,
			subtitle: toast.subtitle ?? undefined,
			variant: toast.variant,
			persistent: toast.persistent ?? false,
		});
	}

	private registerCommand(params: CommandRegisterParams): JsonValue {
		const canonicalId = toCanonicalId(this.manifest.manifest.id, params.id);
		if (
			this.commandDisposers.has(params.id) ||
			this.context.commands
				.getAll()
				.some((command) => command.name === canonicalId)
		) {
			throw this.conflict("command", canonicalId);
		}
		const command: Command = {
			name: canonicalId,
			displayName: params.id,
			description: params.description,
			argName: params.argName ?? undefined,
			category: params.category ?? undefined,
			execute: async (commandContext) => {
				try {
					await this.requestPlugin(
						"kit/commands/execute",
						{ id: params.id, args: commandContext.args },
						"NullResult",
					);
				} catch (error) {
					this.context.ui.toast({
						title: `/${params.id} failed`,
						subtitle: safeErrorMessage(error),
						variant: "error",
					});
				}
			},
		};
		this.commandDisposers.set(
			params.id,
			this.context.commands.register(command),
		);
		return { id: canonicalId };
	}

	private registerTool(params: ToolRegisterParams): JsonValue {
		const canonicalId = toCanonicalId(this.manifest.manifest.id, params.id);
		const modelName = toModelToolName(this.manifest.manifest.id, params.id);
		if (modelName.length > 64) {
			throw new JsonRpcError(
				-32602,
				"Derived model tool name exceeds 64 characters",
			);
		}
		if (
			this.toolDisposers.has(params.id) ||
			this.context.runtime.getTools().some((tool) => tool.name === modelName)
		) {
			throw this.conflict("tool", canonicalId);
		}

		const inputSchema = params.inputSchema as unknown as TSchema;
		const tool: AgentTool = {
			name: modelName,
			label: params.label ?? params.id,
			description: params.description,
			parameters: inputSchema,
			executionMode: params.executionMode ?? "sequential",
			execute: async (toolCallId, input, signal) => {
				if (!Check(inputSchema, input)) {
					throw new Error(`Tool input failed validation for ${canonicalId}`);
				}
				const result = (await this.requestPlugin(
					"kit/tools/execute",
					{
						id: params.id,
						toolCallId,
						input: input as Record<string, JsonValue>,
					},
					"ToolExecuteResult",
					signal,
				)) as unknown as ToolExecuteResult;
				return {
					content: result.content,
					details: result.details ?? null,
					terminate: result.terminate ?? false,
				};
			},
		};
		this.toolDisposers.set(
			params.id,
			this.context.runtime.addTool(
				Object.assign(tool, {
					promptSnippet: params.promptSnippet ?? undefined,
					promptGuidelines: params.promptGuidelines ?? [],
				}),
			),
		);
		return { id: canonicalId, modelName };
	}

	private registerInterceptor(): null {
		if (this.interceptorDisposer) return null;
		this.interceptorDisposer = this.context.runtime.addToolApprovalHandler(
			async (request, signal) => {
				const result = (await this.requestPlugin(
					"kit/tool-calls/before-execute",
					{ toolCall: this.toToolCall(request) },
					"ToolCallDecision",
					signal,
				)) as unknown as
					| { action: "allow" }
					| { action: "reject-and-continue"; message?: string };
				return result.action === "allow"
					? { approved: true }
					: { approved: false, reason: result.message };
			},
		);
		return null;
	}

	private toToolCall(request: ToolApprovalRequest): JsonValue {
		if (!isRecord(request.args)) {
			throw new Error(`Tool call ${request.toolCallId} has non-object input`);
		}
		return {
			id: request.toolCallId,
			name: request.toolName,
			input: request.args,
		};
	}

	private async confirm(params: JsonValue | undefined, signal: AbortSignal) {
		const input = params as unknown as {
			title: string;
			message?: string | null;
			confirmLabel?: string | null;
			cancelLabel?: string | null;
			defaultValue?: boolean;
		};
		return (
			(await this.context.ui.confirm({
				title: input.title,
				message: input.message ?? undefined,
				confirmLabel: input.confirmLabel ?? undefined,
				cancelLabel: input.cancelLabel ?? undefined,
				defaultValue: input.defaultValue,
				signal,
			})) ?? false
		);
	}

	private async input(params: JsonValue | undefined, signal: AbortSignal) {
		const input = params as unknown as {
			title: string;
			message?: string | null;
			placeholder?: string | null;
			initialValue?: string | null;
		};
		return (
			(await this.context.ui.input({
				title: input.title,
				message: input.message ?? undefined,
				placeholder: input.placeholder ?? undefined,
				initialValue: input.initialValue ?? undefined,
				signal,
			})) ?? null
		);
	}

	private async select(params: SelectParams, signal: AbortSignal) {
		const selected = await this.context.ui.select<JsonValue>({
			title: params.title,
			message: params.message ?? undefined,
			options: params.options.map((option) => ({
				label: option.label,
				value: option.value,
				description: option.description ?? undefined,
			})),
			filterable: params.filterable ?? false,
			placeholder: params.placeholder ?? undefined,
			signal,
		});
		return selected === undefined ? null : { value: selected };
	}

	private setChrome(
		area: "footer" | "header",
		params: ChromeSetParams,
	): JsonValue {
		const canonicalId = toCanonicalId(this.manifest.manifest.id, params.id);
		const controller = this.context[area];
		const ids = area === "header" ? this.headerIds : this.footerIds;
		controller.setContribution({
			id: canonicalId,
			content: toChromeContent(params),
			side: params.side ?? "right",
			onClick: params.clickable
				? async () => {
						try {
							await this.requestPlugin(
								`kit/${area}/click`,
								{ id: params.id },
								"NullResult",
							);
						} catch (error) {
							this.context.ui.toast({
								title: `${canonicalId} click failed`,
								subtitle: safeErrorMessage(error),
								variant: "error",
							});
						}
					}
				: undefined,
		});
		ids.add(canonicalId);
		return { id: canonicalId };
	}

	private clearChrome(
		area: "footer" | "header",
		params: JsonValue | undefined,
	): null {
		const localId = (params as unknown as { id: string }).id;
		const canonicalId = toCanonicalId(this.manifest.manifest.id, localId);
		this.context[area].clearContribution(canonicalId);
		(area === "header" ? this.headerIds : this.footerIds).delete(canonicalId);
		return null;
	}

	private hideChrome(
		area: "footer" | "header",
		params: JsonValue | undefined,
	): null {
		const canonicalId = (params as unknown as { id: string }).id;
		const claims =
			area === "header" ? this.headerHideClaims : this.footerHideClaims;
		if (!claims.has(canonicalId)) {
			claims.set(canonicalId, this.context[area].hideContribution(canonicalId));
		}
		return null;
	}

	private showChrome(
		area: "footer" | "header",
		params: JsonValue | undefined,
	): null {
		const canonicalId = (params as unknown as { id: string }).id;
		const claims =
			area === "header" ? this.headerHideClaims : this.footerHideClaims;
		claims.get(canonicalId)?.();
		claims.delete(canonicalId);
		return null;
	}

	private registerSubagent(params: SubagentRegisterParams): JsonValue {
		const canonicalId = toCanonicalId(this.manifest.manifest.id, params.id);
		if (
			this.subagentDisposers.has(params.id) ||
			this.context.runtime
				.getAllSubagents()
				.some((subagent) => subagent.name === canonicalId)
		) {
			throw this.conflict("subagent", canonicalId);
		}
		this.subagentDisposers.set(
			params.id,
			this.context.runtime.addPluginSubagent({
				name: canonicalId,
				description: params.description,
				instructions: params.instructions,
				model: params.model ?? undefined,
				source: "plugin",
				pluginName: this.manifest.manifest.id,
			}),
		);
		return { id: canonicalId };
	}

	private async submitMessage(params: JsonValue | undefined): Promise<null> {
		const input = params as unknown as { sessionId: string; text: string };
		if (this.context.runtime.getSession().id !== input.sessionId) {
			throw new JsonRpcError(-32004, "Active session changed");
		}
		await this.context.runtime.submitMessage(input.text);
		return null;
	}

	private unregister(
		registrations: Map<string, () => void>,
		params: JsonValue | undefined,
	): null {
		const id = (params as unknown as { id: string }).id;
		registrations.get(id)?.();
		registrations.delete(id);
		return null;
	}

	private conflict(type: string, id: string): JsonRpcError {
		return new JsonRpcError(-32003, `${type} ${id} is already registered`, {
			type,
			id,
		});
	}

	private requestPlugin(
		method: string,
		params: JsonValue | undefined,
		resultDefinition: string,
		signal?: AbortSignal,
	): Promise<JsonValue> {
		if (!this.ready) {
			return Promise.reject(new JsonRpcError(-32002, "Plugin is not ready"));
		}
		return this.requireEndpoint().request(method, params, {
			signal,
			validateResult:
				resultDefinition === "ToolExecuteResult"
					? validateToolExecuteResult
					: resultDefinition === "ToolCallDecision"
						? validateToolCallDecision
						: validateNullResult,
		});
	}

	private requireReady(): void {
		if (this.state === "ready") return;
		const error = new JsonRpcError(-32002, "Endpoint is not ready");
		if (this.state === "initializing") this.protocolFailure(error);
		throw error;
	}

	private requireEndpoint(): JsonRpcEndpoint {
		if (!this.endpoint) throw new Error("Plugin endpoint is not available");
		return this.endpoint;
	}

	private protocolFailure(error: Error): void {
		if (this.state === "stopping" || this.state === "stopped") return;
		this.reportFailure("protocol", error);
		void this.stop(false);
	}

	private handleExit(code: number | null, signal: NodeJS.Signals | null): void {
		this.exitCode = code;
		this.exitSignal = signal;
		this.resolveExit?.();
		this.resolveExit = null;
		this.endpoint?.close(new JsonRpcError(-32002, "Plugin process exited"));

		if (this.state === "stopping" || this.state === "stopped") {
			this.state = "stopped";
			return;
		}
		const phase = this.state === "initializing" ? "initialize" : "runtime";
		this.removeContributions();
		this.state = "stopped";
		this.reportFailure(phase, new Error("Plugin process exited unexpectedly"));
	}

	private kill(signal: NodeJS.Signals): void {
		const proc = this.process;
		if (!proc?.pid) return;
		try {
			if (process.platform === "win32") proc.kill(signal);
			else process.kill(-proc.pid, signal);
		} catch {
			try {
				proc.kill(signal);
			} catch {
				// The process has already exited.
			}
		}
	}

	private appendStderr(chunk: Buffer): void {
		this.stderrTail = Buffer.concat([this.stderrTail, chunk]);
		if (this.stderrTail.byteLength > STDERR_TAIL_BYTES) {
			this.stderrTail = this.stderrTail.subarray(
				this.stderrTail.byteLength - STDERR_TAIL_BYTES,
			);
		}
	}

	private finalStderrLine(): string | undefined {
		const lines = this.stderrTail.toString("utf8").trim().split(/\r?\n/);
		const line = sanitizeDiagnostic(lines.at(-1) ?? "").trim();
		return line ? line.slice(0, SAFE_ERROR_LENGTH) : undefined;
	}

	private reportFailure(
		phase: ExternalPluginFailure["phase"],
		error: unknown,
	): void {
		if (this.failureReported) return;
		this.failureReported = true;
		this.onFailure({
			source: this.manifest.source,
			phase,
			pluginId: this.manifest.manifest.id,
			manifestPath: this.manifest.manifestPath,
			message: safeErrorMessage(error),
			exitCode: this.exitCode,
			exitSignal: this.exitSignal,
			stderr: this.finalStderrLine(),
		});
	}

	private removeContributions(): void {
		for (const disposer of this.commandDisposers.values()) disposer();
		for (const disposer of this.toolDisposers.values()) disposer();
		for (const disposer of this.subagentDisposers.values()) disposer();
		this.commandDisposers.clear();
		this.toolDisposers.clear();
		this.subagentDisposers.clear();
		this.interceptorDisposer?.();
		this.interceptorDisposer = null;
		this.systemPromptSlot?.dispose();
		this.systemPromptSlot = null;

		for (const id of this.headerIds) this.context.header.clearContribution(id);
		for (const id of this.footerIds) this.context.footer.clearContribution(id);
		this.headerIds.clear();
		this.footerIds.clear();
		for (const dispose of this.headerHideClaims.values()) dispose();
		for (const dispose of this.footerHideClaims.values()) dispose();
		this.headerHideClaims.clear();
		this.footerHideClaims.clear();
	}
}

export { publicTurn };
