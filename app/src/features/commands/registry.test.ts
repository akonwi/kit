import { describe, expect, test } from "bun:test";
import { createCommandRegistry } from "./registry";
import type { Command } from "./types";

function command(name: string): Command {
	return {
		name,
		description: name,
		execute: () => undefined,
	};
}

describe("createCommandRegistry", () => {
	test("notifies subscribers when commands are registered and disposed", () => {
		const registry = createCommandRegistry([command("initial")]);
		let notifications = 0;
		const unsubscribe = registry.subscribe(() => {
			notifications += 1;
		});

		const dispose = registry.register(command("dynamic"));
		expect(notifications).toBe(1);
		expect(registry.getAll().map((entry) => entry.name)).toEqual([
			"initial",
			"dynamic",
		]);

		dispose();
		expect(notifications).toBe(2);
		expect(registry.getAll().map((entry) => entry.name)).toEqual(["initial"]);

		unsubscribe();
		registry.register(command("silent"));
		expect(notifications).toBe(2);
	});
});
