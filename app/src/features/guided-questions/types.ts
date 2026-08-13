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

export type GuidedQuestionsResult = {
	cancelled: boolean;
	answers: Record<string, AnswerValue>;
};

export type GuidedQuestionsRequester = {
	activate(
		params: GuidedQuestionsInput,
		signal?: AbortSignal,
	): Promise<GuidedQuestionsResult>;
};

const RESERVED_QUESTION_IDS = new Set([
	"__proto__",
	"constructor",
	"prototype",
]);

export function normalizeQuestion(
	raw: Record<string, unknown>,
	index: number,
): GuidedQuestion {
	const requestedKind: QuestionKind =
		raw.kind === "select" ||
		raw.kind === "multiselect" ||
		raw.kind === "boolean" ||
		raw.kind === "text"
			? raw.kind
			: "text";
	const options = Array.isArray(raw.options)
		? raw.options
				.map((value: unknown) => String(value || "").trim())
				.filter(Boolean)
		: undefined;
	const kind =
		(requestedKind === "select" || requestedKind === "multiselect") &&
		!options?.length
			? "text"
			: requestedKind;
	const fallbackId = `q${index + 1}`;
	const requestedId = String(raw.id || fallbackId).trim() || fallbackId;
	const id = RESERVED_QUESTION_IDS.has(requestedId) ? fallbackId : requestedId;
	const label = String(raw.label || "").trim() || `Question ${index + 1}`;

	return {
		id,
		kind,
		label,
		help: typeof raw.help === "string" ? raw.help.trim() : undefined,
		placeholder:
			typeof raw.placeholder === "string" ? raw.placeholder : undefined,
		required: raw.required !== false,
		options,
	};
}

export function normalizeQuestions(
	rawQuestions: readonly unknown[],
): GuidedQuestion[] {
	const seen = new Set<string>();
	return rawQuestions.map((value, index) => {
		const raw =
			value && typeof value === "object"
				? (value as Record<string, unknown>)
				: {};
		const question = normalizeQuestion(raw, index);
		let id = question.id;
		let suffix = 2;
		while (seen.has(id)) {
			id = `${question.id}-${suffix}`;
			suffix += 1;
		}
		seen.add(id);
		return id === question.id ? question : { ...question, id };
	});
}
