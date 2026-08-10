import { describe, expect, test } from "bun:test";
import type { RemoteInteractionEvent } from "./remote-interaction-broker";
import {
	MAX_PENDING_OPEN_URLS,
	RemoteInteractionBroker,
} from "./remote-interaction-broker";

function requestedEvent(events: RemoteInteractionEvent[]) {
	const event = events.find((candidate) => candidate.type === "ui_request");
	if (!event || event.type !== "ui_request") {
		throw new Error("Expected a ui_request event");
	}
	return event;
}

describe("RemoteInteractionBroker", () => {
	test("resolves the first valid confirmation response and broadcasts resolution", async () => {
		const broker = new RemoteInteractionBroker();
		const events: RemoteInteractionEvent[] = [];
		broker.connectClient();
		broker.subscribe((event) => events.push(event));

		const result = broker.confirm({
			title: "Proceed?",
			message: "Run the command",
		});
		const request = requestedEvent(events).request;
		expect(request).toMatchObject({
			kind: "confirm",
			payload: { title: "Proceed?", message: "Run the command" },
		});
		expect(broker.respond(request.id, { confirmed: "yes" })).toEqual({
			accepted: false,
			error: "Confirm response must contain a boolean confirmed value",
		});
		expect(broker.respond(request.id, { confirmed: true })).toEqual({
			accepted: true,
		});
		expect(broker.respond(request.id, { confirmed: false })).toEqual({
			accepted: false,
			error: "Interaction is no longer pending",
		});
		expect(await result).toBe(true);
		expect(events.at(-1)).toEqual({
			type: "ui_resolved",
			generation: 2,
			requestId: request.id,
			kind: "confirm",
			resolution: "answered",
			response: { confirmed: true },
		});
		broker.dispose();
	});

	test("keeps requests pending and replays them to another client", async () => {
		const broker = new RemoteInteractionBroker();
		const result = broker.input({ title: "Name" });
		let resolved = false;
		void result.then(() => {
			resolved = true;
		});
		await Bun.sleep(20);
		expect(resolved).toBe(false);

		const replay = broker.connectClient();
		expect(replay).toHaveLength(1);
		expect(replay[0]).toMatchObject({
			type: "ui_snapshot",
			generation: 1,
			requests: [{ kind: "input", payload: { title: "Name" } }],
		});
		if (replay[0]?.type !== "ui_snapshot" || !replay[0].requests[0]) {
			throw new Error("Expected interaction snapshot");
		}
		expect(broker.respond(replay[0].requests[0].id, { value: "Ada" })).toEqual({
			accepted: true,
		});
		expect(await result).toBe("Ada");
		expect(broker.getPendingSnapshot()).toEqual({
			generation: 2,
			requests: [],
		});
		broker.dispose();
	});

	test("queues HTTP plugin open actions without blocking the plugin", async () => {
		const broker = new RemoteInteractionBroker();
		const events: unknown[] = [];
		broker.subscribe((event) => events.push(event));
		await expect(
			broker.openUrl({
				url: "https://example.com/docs",
				source: "docs-plugin",
			}),
		).resolves.toBeUndefined();
		const request = broker.getPendingRequests()[0];
		if (!request) throw new Error("Expected open URL request");

		expect(request).toMatchObject({
			kind: "open_url",
			payload: {
				title: "Open link",
				url: "https://example.com/docs",
				source: "docs-plugin",
			},
		});
		expect(broker.respond(request.id, { opened: true })).toEqual({
			accepted: true,
		});
		expect(events.at(-1)).toMatchObject({
			type: "ui_resolved",
			kind: "open_url",
			response: { opened: true },
		});
		broker.dispose();
	});

	test("bounds and deduplicates pending plugin open actions", async () => {
		const broker = new RemoteInteractionBroker();
		for (let index = 0; index < MAX_PENDING_OPEN_URLS + 2; index += 1) {
			await broker.openUrl({ url: `https://example.com/${index}` });
		}
		const pending = broker.getPendingRequests();
		expect(pending).toHaveLength(MAX_PENDING_OPEN_URLS);
		expect(pending[0]?.payload.url).toBe("https://example.com/2");

		const generation = broker.getPendingSnapshot().generation;
		await broker.openUrl({
			url: `https://example.com/${MAX_PENDING_OPEN_URLS + 1}`,
		});
		expect(broker.getPendingSnapshot()).toMatchObject({
			generation,
			requests: pending,
		});

		const aborted = new AbortController();
		aborted.abort();
		await broker.openUrl({
			url: "https://example.com/aborted",
			signal: aborted.signal,
		});
		expect(broker.getPendingSnapshot()).toMatchObject({
			generation,
			requests: pending,
		});
		broker.dispose();
	});

	test("rejects unsafe remote plugin URLs", async () => {
		const broker = new RemoteInteractionBroker();
		await expect(
			broker.openUrl({ url: "file:///tmp/private" }),
		).rejects.toThrow("Only HTTP and HTTPS URLs can open remotely");
		expect(broker.getPendingRequests()).toEqual([]);
		broker.dispose();
	});

	test("maps opaque select option ids back to in-process values", async () => {
		const broker = new RemoteInteractionBroker();
		const events: RemoteInteractionEvent[] = [];
		broker.connectClient();
		broker.subscribe((event) => events.push(event));
		const expected = { provider: "test", model: "one" };

		const result = broker.select({
			title: "Model",
			options: [{ label: "One", value: expected, description: "First model" }],
		});
		const request = requestedEvent(events).request;
		expect(request.payload.options).toEqual([
			{ id: "0", label: "One", description: "First model" },
		]);
		expect(broker.respond(request.id, { optionId: "model-one" })).toEqual({
			accepted: false,
			error: "Selected option does not exist",
		});
		expect(broker.respond(request.id, { optionId: "0" })).toEqual({
			accepted: true,
		});
		expect(await result).toBe(expected);
		broker.dispose();
	});

	test("validates guided-question answers", async () => {
		const broker = new RemoteInteractionBroker();
		const events: RemoteInteractionEvent[] = [];
		broker.connectClient();
		broker.subscribe((event) => events.push(event));
		const result = broker.activate({
			title: "Setup",
			questions: [
				{
					id: "runtime",
					kind: "select",
					label: "Runtime",
					required: true,
					options: ["Bun", "Node"],
				},
				{
					id: "notes",
					kind: "text",
					label: "Notes",
					required: false,
				},
			],
		});
		const request = requestedEvent(events).request;
		expect(
			broker.respond(request.id, {
				cancelled: false,
				answers: { runtime: "Deno" },
			}),
		).toEqual({
			accepted: false,
			error: "Answer runtime contains an unknown option",
		});
		expect(
			broker.respond(request.id, {
				cancelled: false,
				answers: { runtime: "Bun", notes: "Fast" },
			}),
		).toEqual({ accepted: true });
		expect(await result).toEqual({
			cancelled: false,
			answers: { runtime: "Bun", notes: "Fast" },
		});
		broker.dispose();
	});

	test("resolves aborted and shutdown requests with safe defaults", async () => {
		const broker = new RemoteInteractionBroker();
		broker.connectClient();
		const abortController = new AbortController();
		const input = broker.input({
			title: "Name",
			signal: abortController.signal,
		});
		const abortedRequest = broker.getPendingRequests()[0];
		if (!abortedRequest) throw new Error("Expected pending input request");
		abortController.abort();
		expect(await input).toBeUndefined();
		expect(broker.respond(abortedRequest.id, { value: "Too late" })).toEqual({
			accepted: false,
			error: "Interaction is no longer pending",
		});

		const confirmation = broker.confirm({ title: "Proceed?" });
		broker.dispose();
		expect(await confirmation).toBe(false);
	});
});
