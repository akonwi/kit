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

		expect(result?.content).toEqual([
			{ type: "text", text: "User confirmed." },
		]);
		expect(result?.details).toEqual({ confirmed: true });
	});

	test("input_from_user returns submitted text to the model", async () => {
		const tool = createTools({ input: async () => "purple banana" }).find(
			(candidate) => candidate.name === "input_from_user",
		);

		const result = await tool?.execute("call", { title: "Name?" });

		expect(result?.content).toEqual([{ type: "text", text: "purple banana" }]);
		expect(result?.details).toEqual({
			value: "purple banana",
			cancelled: false,
		});
	});

	test("input_from_user returns null when cancelled", async () => {
		const tool = createTools({ input: async () => undefined }).find(
			(candidate) => candidate.name === "input_from_user",
		);

		const result = await tool?.execute("call", { title: "Name?" });

		expect(result?.content).toEqual([
			{ type: "text", text: "Input cancelled." },
		]);
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

		expect(result?.content).toEqual([{ type: "text", text: "prod" }]);
		expect(result?.details).toEqual({
			value: "prod",
			label: "Production",
			cancelled: false,
		});
	});
});
