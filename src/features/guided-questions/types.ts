export type QuestionKind = "text" | "select" | "multiselect" | "boolean";

export type GuidedQuestion = {
	id: string;
	kind: QuestionKind;
	label: string;
	help?: string;
	placeholder?: string;
	required: boolean;
	options?: string[];
};

export type GuidedQuestionsInput = {
	title?: string;
	intro?: string;
	questions: GuidedQuestion[];
};

export type AnswerValue = string | string[] | boolean;

export function normalizeQuestion(
	raw: Record<string, unknown>,
	index: number,
): GuidedQuestion {
	const kind: QuestionKind =
		raw.kind === "select" ||
		raw.kind === "multiselect" ||
		raw.kind === "boolean" ||
		raw.kind === "text"
			? raw.kind
			: "text";

	const id = String(raw.id || `q${index + 1}`).trim() || `q${index + 1}`;
	const label = String(raw.label || "").trim() || `Question ${index + 1}`;

	return {
		id,
		kind,
		label,
		help: typeof raw.help === "string" ? raw.help.trim() : undefined,
		placeholder:
			typeof raw.placeholder === "string" ? raw.placeholder : undefined,
		required: raw.required !== false,
		options: Array.isArray(raw.options)
			? raw.options.map((s: unknown) => String(s || "").trim()).filter(Boolean)
			: undefined,
	};
}
