import { describe, expect, test } from "bun:test";
import { authPromptAllowsEmpty, buildProviderOptions } from "./LoginModal";

describe("provider login options", () => {
	test("uses provider-owned API key and OAuth flows", () => {
		const options = buildProviderOptions();
		expect(
			options.some(
				(option) =>
					option.providerId === "anthropic" && option.authType === "api_key",
			),
		).toBe(true);
		expect(
			options.some(
				(option) =>
					option.providerId === "anthropic" && option.authType === "oauth",
			),
		).toBe(true);
		expect(
			options.some(
				(option) =>
					option.providerId === "openai-codex" && option.authType === "oauth",
			),
		).toBe(true);
	});

	test("allows blank text confirmations but requires secrets and selections", () => {
		expect(
			authPromptAllowsEmpty({ type: "text", message: "Press Enter" }),
		).toBe(true);
		expect(
			authPromptAllowsEmpty({ type: "manual_code", message: "Paste code" }),
		).toBe(true);
		expect(authPromptAllowsEmpty({ type: "secret", message: "API key" })).toBe(
			false,
		);
		expect(
			authPromptAllowsEmpty({
				type: "select",
				message: "Choose",
				options: [],
			}),
		).toBe(false);
	});
});
