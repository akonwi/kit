import { describe, expect, test } from "bun:test";
import { PassThrough } from "node:stream";
import { JsonRpcEndpoint, JsonRpcError, type JsonValue } from "./json-rpc";

function endpointPair(options?: {
	handleA?: (
		method: string,
		params: JsonValue | undefined,
	) => JsonValue | Promise<JsonValue>;
	handleB?: (
		method: string,
		params: JsonValue | undefined,
	) => JsonValue | Promise<JsonValue>;
}) {
	const aToB = new PassThrough();
	const bToA = new PassThrough();
	const fatals: Error[] = [];
	const a = new JsonRpcEndpoint({
		input: bToA,
		output: aToB,
		requestIdPrefix: "a",
		handleRequest: (method, params) =>
			options?.handleA?.(method, params) ?? null,
		handleNotification: () => {},
		onFatal: (error) => fatals.push(error),
	});
	const b = new JsonRpcEndpoint({
		input: aToB,
		output: bToA,
		requestIdPrefix: "b",
		handleRequest: (method, params) =>
			options?.handleB?.(method, params) ?? null,
		handleNotification: () => {},
		onFatal: (error) => fatals.push(error),
	});
	return { a, b, fatals };
}

describe("JsonRpcEndpoint", () => {
	test("supports concurrent out-of-order responses", async () => {
		const resolvers = new Map<string, (value: JsonValue) => void>();
		const pair = endpointPair({
			handleB: (method) =>
				new Promise((resolve) => resolvers.set(method, resolve)),
		});

		const first = pair.a.request("unknown/first", { value: 1 });
		const second = pair.a.request("unknown/second", { value: 2 });
		await Bun.sleep(0);
		resolvers.get("unknown/second")?.("second");
		resolvers.get("unknown/first")?.("first");

		expect(await second).toBe("second");
		expect(await first).toBe("first");
		expect(pair.fatals).toEqual([]);
		pair.a.close();
		pair.b.close();
	});

	test("keeps reading while a handler makes a nested request", async () => {
		let b: JsonRpcEndpoint;
		const pair = endpointPair({
			handleA: (method) =>
				method === "unknown/nested" ? "nested-result" : null,
			handleB: async (method) => {
				if (method !== "unknown/outer") return null;
				return b.request("unknown/nested");
			},
		});
		b = pair.b;

		expect(await pair.a.request("unknown/outer")).toBe("nested-result");
		expect(pair.fatals).toEqual([]);
		pair.a.close();
		pair.b.close();
	});

	test("accepts CRLF, empty lines, and batches", async () => {
		const input = new PassThrough();
		const output = new PassThrough();
		const frames: string[] = [];
		output.setEncoding("utf8");
		output.on("data", (chunk: string) => frames.push(chunk));
		const endpoint = new JsonRpcEndpoint({
			input,
			output,
			requestIdPrefix: "kit",
			handleRequest: (_method, params) => params ?? null,
			handleNotification: () => {},
			onFatal: (error) => {
				throw error;
			},
		});

		input.write(
			'\r\n[{"jsonrpc":"2.0","id":"one","method":"unknown/echo","params":{"n":1}},{"jsonrpc":"2.0","method":"unknown/ignored"}]\r\n',
		);
		await Bun.sleep(5);
		const response = JSON.parse(frames.join("").trim());
		expect(response).toEqual([{ jsonrpc: "2.0", id: "one", result: { n: 1 } }]);
		endpoint.close();
	});

	test("propagates cancellation in both directions", async () => {
		let handlerSignal: AbortSignal | undefined;
		const aToB = new PassThrough();
		const bToA = new PassThrough();
		const a = new JsonRpcEndpoint({
			input: bToA,
			output: aToB,
			requestIdPrefix: "a",
			handleRequest: () => null,
			handleNotification: () => {},
			onFatal: () => {},
		});
		const b = new JsonRpcEndpoint({
			input: aToB,
			output: bToA,
			requestIdPrefix: "b",
			handleRequest: (_method, _params, signal) => {
				handlerSignal = signal;
				return new Promise<JsonValue>((resolve) => {
					signal.addEventListener("abort", () => resolve(null), { once: true });
				});
			},
			handleNotification: () => {},
			onFatal: () => {},
		});
		const abort = new AbortController();
		const request = a.request("unknown/slow", undefined, {
			signal: abort.signal,
		});
		await Bun.sleep(0);
		abort.abort();

		await expect(request).rejects.toMatchObject({ code: -32001 });
		await Bun.sleep(0);
		expect(handlerSignal?.aborted).toBe(true);
		a.close();
		b.close();
	});

	test("quarantines an endpoint when a cancelled incoming handler does not stop", async () => {
		const aToB = new PassThrough();
		const bToA = new PassThrough();
		const fatals: Error[] = [];
		const a = new JsonRpcEndpoint({
			input: bToA,
			output: aToB,
			requestIdPrefix: "a",
			handleRequest: () => null,
			handleNotification: () => {},
			onFatal: () => {},
		});
		const b = new JsonRpcEndpoint({
			input: aToB,
			output: bToA,
			requestIdPrefix: "b",
			incomingCancellationGraceMs: 5,
			handleRequest: () => new Promise(() => {}),
			handleNotification: () => {},
			onFatal: (error) => fatals.push(error),
		});
		const abort = new AbortController();
		const request = a.request("plugin/hang", undefined, {
			signal: abort.signal,
		});
		await Bun.sleep(0);
		abort.abort();
		await expect(request).rejects.toMatchObject({ code: -32001 });
		await Bun.sleep(10);
		expect(fatals.at(-1)?.message).toBe(
			"Cancelled JSON-RPC handler did not stop",
		);
		a.close();
		b.close();
	});

	test("forgets cancelled pending requests and ignores one late response", async () => {
		const input = new PassThrough();
		const output = new PassThrough();
		const fatals: Error[] = [];
		const endpoint = new JsonRpcEndpoint({
			input,
			output,
			requestIdPrefix: "kit",
			handleRequest: () => null,
			handleNotification: () => {},
			onFatal: (error) => fatals.push(error),
		});
		const abort = new AbortController();
		const request = endpoint.request("plugin/slow", undefined, {
			signal: abort.signal,
		});
		await Bun.sleep(0);
		abort.abort();
		await expect(request).rejects.toMatchObject({ code: -32001 });

		input.write('{"jsonrpc":"2.0","id":"kit-1","result":null}\n');
		await Bun.sleep(0);
		expect(fatals).toEqual([]);
		endpoint.close();
	});

	test("fails malformed stdout and enforces the pending request limit", async () => {
		const input = new PassThrough();
		const output = new PassThrough();
		const fatals: Error[] = [];
		let release: (() => void) | undefined;
		const endpoint = new JsonRpcEndpoint({
			input,
			output,
			requestIdPrefix: "kit",
			maxIncomingRequests: 1,
			handleRequest: () =>
				new Promise<JsonValue>((resolve) => {
					release = () => resolve(null);
				}),
			handleNotification: () => {},
			onFatal: (error) => fatals.push(error),
		});
		const lines: string[] = [];
		output.setEncoding("utf8");
		output.on("data", (chunk: string) => lines.push(chunk));

		input.write(
			'{"jsonrpc":"2.0","id":"one","method":"unknown/slow"}\n{"jsonrpc":"2.0","id":"two","method":"unknown/slow"}\n',
		);
		await Bun.sleep(5);
		expect(lines.join("")).toContain('"code":-32006');
		release?.();
		await Bun.sleep(0);
		input.write("not-json\n");
		await Bun.sleep(5);
		expect(fatals.at(-1)?.message).toContain("malformed JSON");
		endpoint.close();
	});

	test("rejects non-UTF-8 protocol output", async () => {
		const input = new PassThrough();
		const output = new PassThrough();
		const fatals: Error[] = [];
		new JsonRpcEndpoint({
			input,
			output,
			requestIdPrefix: "kit",
			handleRequest: () => null,
			handleNotification: () => {},
			onFatal: (error) => fatals.push(error),
		});

		input.write(Buffer.from([0x7b, 0x22, 0x78, 0x22, 0x3a, 0xff, 0x7d, 0x0a]));
		await Bun.sleep(5);

		expect(fatals.at(-1)?.message).toContain("invalid UTF-8");
	});

	test("returns invalid params without terminating the endpoint", async () => {
		const input = new PassThrough();
		const output = new PassThrough();
		const fatals: Error[] = [];
		let handled = false;
		const endpoint = new JsonRpcEndpoint({
			input,
			output,
			requestIdPrefix: "kit",
			handleRequest: () => {
				handled = true;
				return null;
			},
			handleNotification: () => {},
			onFatal: (error) => fatals.push(error),
		});
		const frames: string[] = [];
		output.setEncoding("utf8");
		output.on("data", (chunk: string) => frames.push(chunk));

		input.write(
			'{"jsonrpc":"2.0","id":"bad","method":"kit/session/submit-message","params":{"sessionId":"session-1","text":""}}\n',
		);
		await Bun.sleep(5);

		expect(JSON.parse(frames.join("").trim())).toEqual({
			jsonrpc: "2.0",
			id: "bad",
			error: { code: -32602, message: "Invalid params" },
		});
		expect(handled).toBe(false);
		expect(fatals).toEqual([]);
		endpoint.close();
	});

	test("rejects invalid result payloads", async () => {
		const pair = endpointPair({ handleB: () => ({ wrong: true }) });
		await expect(
			pair.a.request("unknown/result", undefined, {
				validateResult: (value) => value === null,
			}),
		).rejects.toThrow("invalid result");
		expect(pair.fatals).toHaveLength(1);
		pair.b.close();
	});

	test("uses typed application errors", () => {
		const error = new JsonRpcError(-32003, "Contribution conflict", {
			id: "speech.toggle",
		});
		expect(error).toMatchObject({
			code: -32003,
			data: { id: "speech.toggle" },
		});
	});
});
