import { describe, expect, test } from "bun:test";
import { ringBell } from "./notifications";

describe("ringBell", () => {
	test("delegates bell and notification effects to the active host", () => {
		const bells: boolean[] = [];
		const notifications: Array<{ message: string; title?: string }> = [];
		ringBell(true, {
			bell: (isError) => bells.push(isError),
			notify: (message, title) => {
				notifications.push({ message, title });
				return true;
			},
			message: "Agent turn failed",
			title: "Kit",
		});
		expect(bells).toEqual([true]);
		expect(notifications).toEqual([
			{ message: "Agent turn failed", title: "Kit" },
		]);
	});
});
