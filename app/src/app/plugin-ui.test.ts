import { describe, expect, test } from "bun:test";
import { measurePluginConfirmMessageHeight } from "./plugin-ui";

describe("plugin confirmation message sizing", () => {
	test("accounts for wrapped and explicit message lines", () => {
		expect(measurePluginConfirmMessageHeight("short", 20, 24)).toBe(1);
		expect(measurePluginConfirmMessageHeight("x".repeat(30), 11, 24)).toBe(3);
		expect(measurePluginConfirmMessageHeight("one\ntwo\nthree", 20, 24)).toBe(
			3,
		);
	});

	test("reserves dock rows and caps long messages", () => {
		expect(measurePluginConfirmMessageHeight("x".repeat(200), 11, 11)).toBe(3);
		expect(measurePluginConfirmMessageHeight("x".repeat(500), 11, 40)).toBe(12);
	});
});
