import { isUtf8 } from "node:buffer";
import type { Readable, Writable } from "node:stream";
import type { TSchema } from "typebox";
import { Check } from "typebox/schema";
import protocolSchema from "../../docs/plugin-protocol/protocol.schema.json";

export type JsonPrimitive = null | boolean | number | string;
export type JsonValue =
	| JsonPrimitive
	| JsonValue[]
	| { [key: string]: JsonValue };

export type JsonRpcRequestId = number | string;

export type JsonRpcRequestHandler = (
	method: string,
	params: JsonValue | undefined,
	signal: AbortSignal,
) => JsonValue | Promise<JsonValue>;

export type JsonRpcNotificationHandler = (
	method: string,
	params: JsonValue | undefined,
) => void | Promise<void>;

export type JsonRpcEndpointOptions = {
	input: Readable;
	output: Writable;
	requestIdPrefix: string;
	handleRequest: JsonRpcRequestHandler;
	handleNotification: JsonRpcNotificationHandler;
	onFatal: (error: Error) => void;
	maxFrameBytes?: number;
	maxQueuedBytes?: number;
	maxIncomingRequests?: number;
	incomingCancellationGraceMs?: number;
};

export type JsonRpcRequestOptions = {
	signal?: AbortSignal;
	validateResult?: (result: JsonValue) => boolean;
	onResult?: (result: JsonValue) => void;
};

type PendingRequest = {
	id: JsonRpcRequestId;
	method: string;
	resolve: (value: JsonValue) => void;
	reject: (error: Error) => void;
	validateResult?: (result: JsonValue) => boolean;
	onResult?: (result: JsonValue) => void;
	abortCleanup?: () => void;
	settled: boolean;
};

type IncomingRequest = {
	abort: AbortController;
};

type QueuedFrame = {
	data: string;
	bytes: number;
	resolve: () => void;
	reject: (error: Error) => void;
};

type JsonRpcErrorObject = {
	code: number;
	message: string;
	data?: JsonValue;
};

type JsonRpcResponse =
	| { jsonrpc: "2.0"; id: JsonRpcRequestId; result: JsonValue }
	| { jsonrpc: "2.0"; id: JsonRpcRequestId | null; error: JsonRpcErrorObject };

const DEFAULT_MAX_FRAME_BYTES = 16 * 1024 * 1024;
const DEFAULT_MAX_QUEUED_BYTES = 32 * 1024 * 1024;
const DEFAULT_MAX_INCOMING_REQUESTS = 128;
const DEFAULT_INCOMING_CANCELLATION_GRACE_MS = 1_000;

const protocolMessageSchema = protocolSchema as TSchema;
const genericSingleMessageSchema = {
	oneOf: [
		{
			type: "object",
			required: ["jsonrpc", "method"],
			properties: {
				jsonrpc: { const: "2.0" },
				id: { $ref: "#/$defs/RequestId" },
				method: { type: "string" },
				params: { $ref: "#/$defs/JsonRpcParams" },
			},
			additionalProperties: false,
		},
		{ $ref: "#/$defs/SuccessResponse" },
		{ $ref: "#/$defs/ErrorResponse" },
	],
};
const genericProtocolMessageSchema = {
	$schema: protocolSchema.$schema,
	$defs: protocolSchema.$defs,
	oneOf: [
		genericSingleMessageSchema,
		{
			type: "array",
			minItems: 1,
			items: genericSingleMessageSchema,
		},
	],
} as TSchema;

function requestKey(id: JsonRpcRequestId): string {
	return `${typeof id}:${id}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isRequestId(value: unknown): value is JsonRpcRequestId {
	return (
		typeof value === "string" ||
		(typeof value === "number" && Number.isSafeInteger(value))
	);
}

function asJsonValue(value: unknown): JsonValue {
	return value as JsonValue;
}

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

export class JsonRpcError extends Error {
	readonly code: number;
	readonly data?: JsonValue;

	constructor(code: number, message: string, data?: JsonValue) {
		super(message);
		this.name = "JsonRpcError";
		this.code = code;
		this.data = data;
	}
}

export class JsonRpcRemoteError extends JsonRpcError {
	constructor(code: number, message: string, data?: JsonValue) {
		super(code, message, data);
		this.name = "JsonRpcRemoteError";
	}
}

export class JsonRpcEndpoint {
	private readonly input: Readable;
	private readonly output: Writable;
	private readonly requestIdPrefix: string;
	private readonly handleRequest: JsonRpcRequestHandler;
	private readonly handleNotification: JsonRpcNotificationHandler;
	private readonly onFatal: (error: Error) => void;
	private readonly maxFrameBytes: number;
	private readonly maxQueuedBytes: number;
	private readonly maxIncomingRequests: number;
	private readonly incomingCancellationGraceMs: number;
	private readonly pendingRequests = new Map<string, PendingRequest>();
	private readonly cancelledRequestKeys = new Set<string>();
	private readonly incomingRequests = new Map<string, IncomingRequest>();
	private readonly writeQueue: QueuedFrame[] = [];
	private readBuffer = Buffer.alloc(0);
	private queuedBytes = 0;
	private writing = false;
	private nextRequestId = 1;
	private closed = false;
	private fatalReported = false;

	constructor(options: JsonRpcEndpointOptions) {
		this.input = options.input;
		this.output = options.output;
		this.requestIdPrefix = options.requestIdPrefix;
		this.handleRequest = options.handleRequest;
		this.handleNotification = options.handleNotification;
		this.onFatal = options.onFatal;
		this.maxFrameBytes = options.maxFrameBytes ?? DEFAULT_MAX_FRAME_BYTES;
		this.maxQueuedBytes = options.maxQueuedBytes ?? DEFAULT_MAX_QUEUED_BYTES;
		this.maxIncomingRequests =
			options.maxIncomingRequests ?? DEFAULT_MAX_INCOMING_REQUESTS;
		this.incomingCancellationGraceMs =
			options.incomingCancellationGraceMs ??
			DEFAULT_INCOMING_CANCELLATION_GRACE_MS;

		this.input.on("data", this.onData);
		this.input.on("error", this.onInputError);
		this.output.on("error", this.onOutputError);
	}

	request(
		method: string,
		params?: JsonValue,
		options: JsonRpcRequestOptions = {},
	): Promise<JsonValue> {
		if (this.closed) {
			return Promise.reject(new JsonRpcError(-32002, "Endpoint is closed"));
		}
		if (options.signal?.aborted) {
			return Promise.reject(new JsonRpcError(-32001, "Request cancelled"));
		}

		const id = `${this.requestIdPrefix}-${this.nextRequestId++}`;
		const message: Record<string, unknown> = { jsonrpc: "2.0", id, method };
		if (params !== undefined) message.params = params;

		return new Promise<JsonValue>((resolve, reject) => {
			const pending: PendingRequest = {
				id,
				method,
				resolve,
				reject,
				validateResult: options.validateResult,
				onResult: options.onResult,
				settled: false,
			};
			this.pendingRequests.set(requestKey(id), pending);

			if (options.signal) {
				const abort = () => {
					if (pending.settled) return;
					pending.settled = true;
					this.pendingRequests.delete(requestKey(id));
					this.rememberCancelledRequest(requestKey(id));
					pending.reject(new JsonRpcError(-32001, "Request cancelled"));
					void this.notify("kit/cancel", { id }).catch(() => {});
				};
				options.signal.addEventListener("abort", abort, { once: true });
				pending.abortCleanup = () =>
					options.signal?.removeEventListener("abort", abort);
			}

			void this.enqueue(message).catch((error) => {
				this.pendingRequests.delete(requestKey(id));
				pending.abortCleanup?.();
				if (!pending.settled) {
					pending.settled = true;
					reject(error instanceof Error ? error : new Error(String(error)));
				}
			});
		});
	}

	notify(method: string, params?: JsonValue): Promise<void> {
		if (this.closed) {
			return Promise.reject(new JsonRpcError(-32002, "Endpoint is closed"));
		}
		const message: Record<string, unknown> = { jsonrpc: "2.0", method };
		if (params !== undefined) message.params = params;
		return this.enqueue(message);
	}

	close(error: Error = new JsonRpcError(-32002, "Endpoint closed")): void {
		if (this.closed) return;
		this.closed = true;
		this.input.off("data", this.onData);
		this.input.off("error", this.onInputError);
		this.output.off("error", this.onOutputError);

		for (const pending of this.pendingRequests.values()) {
			pending.abortCleanup?.();
			if (!pending.settled) {
				pending.settled = true;
				pending.reject(error);
			}
		}
		this.pendingRequests.clear();
		this.cancelledRequestKeys.clear();

		for (const incoming of this.incomingRequests.values()) {
			incoming.abort.abort();
		}
		this.incomingRequests.clear();

		for (const frame of this.writeQueue.splice(0)) frame.reject(error);
		this.queuedBytes = 0;
	}

	cancelPendingRequests(
		error: Error = new JsonRpcError(-32001, "Request cancelled"),
	): void {
		for (const pending of [...this.pendingRequests.values()]) {
			if (pending.settled) continue;
			pending.settled = true;
			const key = requestKey(pending.id);
			this.pendingRequests.delete(key);
			this.rememberCancelledRequest(key);
			pending.abortCleanup?.();
			pending.reject(error);
			void this.notify("kit/cancel", { id: pending.id }).catch(() => {});
		}
	}

	private readonly onData = (chunk: Buffer | string): void => {
		if (this.closed) return;
		const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk, "utf8");
		this.readBuffer = Buffer.concat([this.readBuffer, bytes]);

		while (!this.closed) {
			const newline = this.readBuffer.indexOf(0x0a);
			if (newline < 0) break;
			const rawLine = this.readBuffer.subarray(0, newline);
			this.readBuffer = this.readBuffer.subarray(newline + 1);
			if (rawLine.byteLength > this.maxFrameBytes) {
				this.fatal(new Error("JSON-RPC frame exceeds 16 MiB"));
				return;
			}
			const line =
				rawLine.at(-1) === 0x0d
					? rawLine.subarray(0, rawLine.byteLength - 1)
					: rawLine;
			if (line.byteLength === 0) continue;
			if (!isUtf8(line)) {
				void this.enqueue({
					jsonrpc: "2.0",
					id: null,
					error: { code: -32700, message: "Parse error" },
				}).finally(() => this.fatal(new Error("Plugin wrote invalid UTF-8")));
				return;
			}
			this.processFrame(line.toString("utf8"));
		}

		if (!this.closed && this.readBuffer.byteLength > this.maxFrameBytes) {
			this.fatal(new Error("JSON-RPC frame exceeds 16 MiB"));
		}
	};

	private readonly onInputError = (error: Error): void => {
		this.fatal(new Error(`JSON-RPC input failed: ${error.message}`));
	};

	private readonly onOutputError = (error: Error): void => {
		this.fatal(new Error(`JSON-RPC output failed: ${error.message}`));
	};

	private processFrame(line: string): void {
		let parsed: unknown;
		try {
			parsed = JSON.parse(line);
		} catch {
			void this.enqueue({
				jsonrpc: "2.0",
				id: null,
				error: { code: -32700, message: "Parse error" },
			}).finally(() => this.fatal(new Error("Plugin wrote malformed JSON")));
			return;
		}

		if (!Check(genericProtocolMessageSchema, parsed)) {
			void this.enqueue({
				jsonrpc: "2.0",
				id: null,
				error: { code: -32600, message: "Invalid request" },
			}).finally(() =>
				this.fatal(new Error("Plugin wrote an invalid JSON-RPC message")),
			);
			return;
		}

		if (Array.isArray(parsed)) {
			this.processBatch(parsed as Array<Record<string, unknown>>);
			return;
		}
		this.processSingle(parsed as Record<string, unknown>);
	}

	private processSingle(message: Record<string, unknown>): void {
		if (typeof message.method !== "string") {
			this.processResponse(message);
			return;
		}
		if (!("id" in message)) {
			this.processNotification(message);
			return;
		}
		void this.processRequest(message).then((response) => {
			if (response) this.enqueueResponse(response);
		});
	}

	private processBatch(messages: Array<Record<string, unknown>>): void {
		const responses: Array<Promise<JsonRpcResponse | null>> = [];
		for (const message of messages) {
			if (typeof message.method !== "string") {
				this.processResponse(message);
				continue;
			}
			if (!("id" in message)) {
				this.processNotification(message);
				continue;
			}
			responses.push(this.processRequest(message));
		}
		if (responses.length === 0) return;
		void Promise.all(responses).then((settled) => {
			const output = settled.filter(
				(response): response is JsonRpcResponse => response !== null,
			);
			if (output.length > 0) this.enqueueResponse(output);
		});
	}

	private processNotification(message: Record<string, unknown>): void {
		if (!Check(protocolMessageSchema, message)) return;
		const method = String(message.method);
		const params =
			"params" in message ? asJsonValue(message.params) : undefined;
		if (method === "kit/cancel") {
			if (isRecord(params) && isRequestId(params.id)) {
				this.incomingRequests.get(requestKey(params.id))?.abort.abort();
			}
			return;
		}
		void Promise.resolve(this.handleNotification(method, params)).catch(
			(error) =>
				this.fatal(
					new Error(
						`JSON-RPC notification handler ${method} failed: ${errorMessage(error)}`,
					),
				),
		);
	}

	private async processRequest(
		message: Record<string, unknown>,
	): Promise<JsonRpcResponse | null> {
		const id = message.id;
		if (!isRequestId(id)) return null;
		if (!Check(protocolMessageSchema, message)) {
			return this.errorResponse(id, -32602, "Invalid params");
		}
		if (this.incomingRequests.size >= this.maxIncomingRequests) {
			return this.errorResponse(id, -32006, "Endpoint is busy");
		}

		const key = requestKey(id);
		if (this.incomingRequests.has(key)) {
			return this.errorResponse(id, -32600, "Duplicate request id");
		}
		const active: IncomingRequest = { abort: new AbortController() };
		this.incomingRequests.set(key, active);

		const params =
			"params" in message ? asJsonValue(message.params) : undefined;
		const handlerExecution = Promise.resolve().then(() =>
			this.handleRequest(String(message.method), params, active.abort.signal),
		);
		let rejectCancellation: ((error: Error) => void) | undefined;
		const cancellation = new Promise<never>((_resolve, reject) => {
			rejectCancellation = reject;
		});
		const abortListener = () =>
			rejectCancellation?.(new JsonRpcError(-32001, "Request cancelled"));
		active.abort.signal.addEventListener("abort", abortListener, {
			once: true,
		});

		try {
			const result = await Promise.race([handlerExecution, cancellation]);
			return { jsonrpc: "2.0", id, result };
		} catch (error) {
			if (error instanceof JsonRpcError) {
				return this.errorResponse(id, error.code, error.message, error.data);
			}
			return this.errorResponse(id, -32603, errorMessage(error));
		} finally {
			active.abort.signal.removeEventListener("abort", abortListener);
			if (
				active.abort.signal.aborted &&
				!(await this.settlesWithin(
					handlerExecution,
					this.incomingCancellationGraceMs,
				))
			) {
				this.fatal(new Error("Cancelled JSON-RPC handler did not stop"));
			}
			this.incomingRequests.delete(key);
		}
	}

	private settlesWithin(
		promise: Promise<unknown>,
		timeoutMs: number,
	): Promise<boolean> {
		return new Promise((resolve) => {
			const timeout = setTimeout(() => resolve(false), timeoutMs);
			promise.then(
				() => {
					clearTimeout(timeout);
					resolve(true);
				},
				() => {
					clearTimeout(timeout);
					resolve(true);
				},
			);
		});
	}

	private processResponse(message: Record<string, unknown>): void {
		const id = message.id;
		if (!isRequestId(id)) {
			this.fatal(
				new Error("Plugin sent a response without a valid request id"),
			);
			return;
		}
		const key = requestKey(id);
		const pending = this.pendingRequests.get(key);
		if (!pending) {
			if (this.cancelledRequestKeys.delete(key)) return;
			this.fatal(new Error(`Plugin sent a response for unknown request ${id}`));
			return;
		}
		this.pendingRequests.delete(key);
		pending.abortCleanup?.();

		if ("error" in message && isRecord(message.error)) {
			if (!pending.settled) {
				pending.settled = true;
				pending.reject(
					new JsonRpcRemoteError(
						Number(message.error.code),
						String(message.error.message),
						"data" in message.error
							? asJsonValue(message.error.data)
							: undefined,
					),
				);
			}
			return;
		}

		const result = asJsonValue(message.result);
		if (pending.validateResult && !pending.validateResult(result)) {
			const error = new Error(
				`Plugin returned an invalid result for ${pending.method}`,
			);
			if (!pending.settled) {
				pending.settled = true;
				pending.reject(error);
			}
			this.fatal(error);
			return;
		}
		pending.onResult?.(result);
		if (!pending.settled) {
			pending.settled = true;
			pending.resolve(result);
		}
	}

	private rememberCancelledRequest(key: string): void {
		this.cancelledRequestKeys.add(key);
		if (this.cancelledRequestKeys.size <= 1024) return;
		const oldest = this.cancelledRequestKeys.values().next().value;
		if (typeof oldest === "string") this.cancelledRequestKeys.delete(oldest);
	}

	private errorResponse(
		id: JsonRpcRequestId | null,
		code: number,
		message: string,
		data?: JsonValue,
	): JsonRpcResponse {
		return {
			jsonrpc: "2.0",
			id,
			error: { code, message, ...(data === undefined ? {} : { data }) },
		};
	}

	private enqueueResponse(response: JsonRpcResponse | JsonRpcResponse[]): void {
		void this.enqueue(response).catch((error) => {
			if (error instanceof JsonRpcError && error.code === -32005) {
				const responses = Array.isArray(response) ? response : [response];
				const limitResponses = responses.map((item) =>
					this.errorResponse(
						item.id,
						-32005,
						"Response exceeds protocol limits",
					),
				);
				const fallback = Array.isArray(response)
					? limitResponses
					: limitResponses[0];
				if (fallback) {
					void this.enqueue(fallback).catch((fallbackError) =>
						this.fatal(
							fallbackError instanceof Error
								? fallbackError
								: new Error(String(fallbackError)),
						),
					);
				}
				return;
			}
			this.fatal(error instanceof Error ? error : new Error(String(error)));
		});
	}

	private enqueue(message: unknown): Promise<void> {
		if (this.closed) {
			return Promise.reject(new JsonRpcError(-32002, "Endpoint is closed"));
		}
		if (!Check(protocolMessageSchema, message)) {
			return Promise.reject(
				new JsonRpcError(
					-32600,
					"Attempted to send an invalid JSON-RPC message",
				),
			);
		}

		const data = `${JSON.stringify(message)}\n`;
		const bytes = Buffer.byteLength(data, "utf8");
		if (bytes > this.maxFrameBytes) {
			const error = new JsonRpcError(-32005, "JSON-RPC frame exceeds 16 MiB");
			return Promise.reject(error);
		}
		if (this.queuedBytes + bytes > this.maxQueuedBytes) {
			const error = new JsonRpcError(
				-32005,
				"JSON-RPC outbound queue exceeds 32 MiB",
			);
			this.fatal(error);
			return Promise.reject(error);
		}

		return new Promise<void>((resolve, reject) => {
			this.writeQueue.push({ data, bytes, resolve, reject });
			this.queuedBytes += bytes;
			this.flushWrites();
		});
	}

	private flushWrites(): void {
		if (this.closed || this.writing) return;
		const frame = this.writeQueue.shift();
		if (!frame) return;
		this.writing = true;
		this.output.write(frame.data, "utf8", (error?: Error | null) => {
			this.writing = false;
			this.queuedBytes -= frame.bytes;
			if (error) {
				frame.reject(error);
				this.fatal(new Error(`JSON-RPC output failed: ${error.message}`));
				return;
			}
			frame.resolve();
			this.flushWrites();
		});
	}

	private fatal(error: Error): void {
		if (this.fatalReported) return;
		this.fatalReported = true;
		this.close(error);
		this.onFatal(error);
	}
}

export function createSchemaValidator(
	definition: string,
): (value: JsonValue) => boolean {
	const schema = {
		$schema: protocolSchema.$schema,
		$defs: protocolSchema.$defs,
		$ref: `#/$defs/${definition}`,
	} as TSchema;
	return (value) => Check(schema, value);
}
