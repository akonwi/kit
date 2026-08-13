import { describe, expect, test } from "bun:test";
import { normalizeQuestion, normalizeQuestions } from "./types";

describe("normalizeQuestion", () => {
	test("normalizes constrained questions without options to text", () => {
		expect(
			normalizeQuestion(
				{ id: "choice", kind: "select", label: "Choose", options: [] },
				0,
			),
		).toMatchObject({ id: "choice", kind: "text", options: [] });
		expect(
			normalizeQuestion(
				{ id: "many", kind: "multiselect", label: "Choose many" },
				1,
			),
		).toMatchObject({ id: "many", kind: "text", options: undefined });
	});

	test("normalizes duplicate and reserved ids deterministically", () => {
		expect(
			normalizeQuestions([
				{ id: "choice", label: "First" },
				{ id: "choice", label: "Second" },
				{ id: "__proto__", label: "Third" },
			]).map((question) => question.id),
		).toEqual(["choice", "choice-2", "q3"]);
	});
});
