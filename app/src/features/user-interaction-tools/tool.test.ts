import { describe, expect, test } from "bun:test";
import { createUserInteractionTools } from "./tool";

function createTools(overrides: {
	confirm?: () => Promise<boolean>;
	input?: () => Promise<string | undefined>;
	select?: () => Promise<string | undefined>;
}) {
	return createUserInteractionTools({
		ui: {
			confirm: overrides.confirm ?? (async () => false),
			input: overrides.input ?? (async () => undefined),
			select: overrides.select ?? (async () => undefined),
		},
		notify: () => {},
	});
}

describe("user interaction tools", () => {
	test("confirm_from_user returns a confirmation boolean", async () => {
		const tool = createTools({ confirm: async () => true }).find(
			(candidate) => candidate.name === "confirm_from_user",
		);

		const result = await tool?.execute("call", {
			title: "Proceed?",
			message: "Run the operation?",
		});

		expect(result?.details).toEqual({ confirmed: true });
	});

	test("input_from_user returns null when cancelled", async () => {
		const tool = createTools({ input: async () => undefined }).find(
			(candidate) => candidate.name === "input_from_user",
		);

		const result = await tool?.execute("call", { title: "Name?" });

		expect(result?.details).toEqual({ value: null, cancelled: true });
	});

	test("select_from_user returns selected value and label", async () => {
		const tool = createTools({ select: async () => "prod" }).find(
			(candidate) => candidate.name === "select_from_user",
		);

		const result = await tool?.execute("call", {
			title: "Environment",
			options: [
				{ label: "Staging", value: "staging" },
				{ label: "Production", value: "prod" },
			],
		});

		expect(result?.details).toEqual({
			value: "prod",
			label: "Production",
			cancelled: false,
		});
	});
});
