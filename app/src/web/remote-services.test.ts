import { describe, expect, test } from "bun:test";
import { WebRemoteServices } from "./remote-services";

describe("web remote attachment services", () => {
	test("keeps the global receiver when using browser fetch", async () => {
		const originalFetch = globalThis.fetch;
		let receiver: unknown;
		globalThis.fetch = function (this: typeof globalThis) {
			receiver = this;
			return Promise.resolve(
				Response.json({ attachment: { id: "attachment-1" } }, { status: 201 }),
			);
		} as unknown as typeof fetch;
		const services = new WebRemoteServices({ command: async () => ({}) });
		globalThis.fetch = originalFetch;

		await expect(
			services.uploadAttachment(new File(["image"], "screenshot.png")),
		).resolves.toBe("attachment-1");
		expect(receiver).toBe(globalThis);
	});
});
