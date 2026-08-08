import { describe, expect, test } from "bun:test";
import { createHeadlessPluginUI } from "./headless-plugin-ui";
import {
	RemoteInteractionBroker,
	type RemoteInteractionEvent,
} from "./remote-interaction-broker";

describe("createHeadlessPluginUI", () => {
	test("returns inert values for UI interactions", async () => {
		const ui = createHeadlessPluginUI();
		expect(
			await ui.select({ title: "Choose", options: ["one", "two"] }),
		).toBeUndefined();
		expect(await ui.input({ title: "Enter" })).toBeUndefined();
		await expect(ui.confirm({ title: "Confirm" })).rejects.toThrow(
			"interactivity is unavailable",
		);
		await expect(
			ui.confirm({ title: "Confirm", defaultValue: true }),
		).rejects.toThrow("interactivity is unavailable");
		expect(await ui.custom(() => undefined)).toBeUndefined();
	});

	test("delegates serializable interactions to the remote broker", async () => {
		const broker = new RemoteInteractionBroker();
		const events: RemoteInteractionEvent[] = [];
		broker.connectClient();
		broker.subscribe((event) => events.push(event));
		const ui = createHeadlessPluginUI(broker);

		const result = ui.confirm({ title: "Confirm" });
		const request = events.find((event) => event.type === "ui_request");
		if (!request || request.type !== "ui_request") {
			throw new Error("Expected ui_request");
		}
		expect(broker.respond(request.request.id, { confirmed: true })).toEqual({
			accepted: true,
		});
		expect(await result).toBe(true);
		broker.dispose();
	});
});
