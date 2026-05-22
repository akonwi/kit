/**
 * Guided questions controller — manages question navigation, answer state,
 * and input mode for the guided questionnaire.
 */

import { createMemo, createSignal } from "solid-js";
import {
	type AnswerValue,
	type GuidedQuestion,
	type GuidedQuestionsInput,
	normalizeQuestion,
} from "./types";

export type GuidedQuestionsMode =
	| "select"
	| "multiselect"
	| "text"
	| "otherText";

export type GuidedQuestionsResult = {
	cancelled: boolean;
	answers: Record<string, AnswerValue>;
};

export function createGuidedQuestionsController() {
	const [active, setActive] = createSignal(false);
	const [questions, setQuestions] = createSignal<GuidedQuestion[]>([]);
	const [title, setTitle] = createSignal("");
	const [intro, setIntro] = createSignal("");
	const [currentIndex, setCurrentIndex] = createSignal(0);
	const [selectIndex, setSelectIndex] = createSignal(0);
	const [mode, setMode] = createSignal<GuidedQuestionsMode>("select");
	const [answers, setAnswers] = createSignal<Record<string, AnswerValue>>({});

	let resolveGuidedQuestions: ((result: GuidedQuestionsResult) => void) | null =
		null;
	const listeners = new Set<(active: boolean) => void>();

	const currentQuestion = createMemo(() => {
		const qs = questions();
		const idx = currentIndex();
		return idx >= 0 && idx < qs.length ? qs[idx] : null;
	});

	const answeredCount = createMemo(() => {
		const qs = questions();
		const ans = answers();
		return qs.filter((q) => {
			const v = ans[q.id];
			if (typeof v === "boolean") return true;
			if (Array.isArray(v)) return v.length > 0;
			return typeof v === "string" && v.trim().length > 0;
		}).length;
	});

	// ── Option helpers ────────────────────────────────────────────

	function getSelectOptions(q: GuidedQuestion): string[] {
		if (q.kind === "boolean") {
			return q.required === false ? ["Yes", "No", "Skip"] : ["Yes", "No"];
		}
		if (q.kind === "select") {
			const provided = Array.isArray(q.options)
				? q.options.filter(Boolean)
				: [];
			return q.required === false ? [...provided, "Skip"] : provided;
		}
		if (q.kind === "multiselect") {
			return Array.isArray(q.options) ? q.options.filter(Boolean) : [];
		}
		return [];
	}

	function getMultiSelectValues(questionId: string): string[] {
		const existing = answers()[questionId];
		return Array.isArray(existing)
			? existing.filter((value): value is string => typeof value === "string")
			: [];
	}

	function getValidSelectIndex(q: GuidedQuestion): number {
		const opts = getSelectOptions(q);
		if (opts.length === 0) return -1;
		const index = selectIndex();
		const clamped = Math.max(0, Math.min(index, opts.length - 1));
		if (clamped !== index) setSelectIndex(clamped);
		return clamped;
	}

	function isOtherOption(value: string): boolean {
		return /^other(\b|\s|:)/i.test(value);
	}

	// ── State loading ─────────────────────────────────────────────

	function loadQuestionState(question = currentQuestion()) {
		const q = question;
		if (!q) return;

		const existing = answers()[q.id];

		if (q.kind === "text") {
			setMode("text");
			return;
		}

		if (q.kind === "multiselect") {
			setMode("multiselect");
			const opts = getSelectOptions(q);
			const selected = getMultiSelectValues(q.id);
			const firstSelected = opts.findIndex((option) =>
				selected.includes(option),
			);
			setSelectIndex(firstSelected >= 0 ? firstSelected : 0);
			return;
		}

		if (q.kind === "boolean") {
			setMode("select");
			const opts = getSelectOptions(q);
			if (existing === true) setSelectIndex(Math.max(0, opts.indexOf("Yes")));
			else if (existing === false)
				setSelectIndex(Math.max(0, opts.indexOf("No")));
			else if (existing === "")
				setSelectIndex(Math.max(0, opts.indexOf("Skip")));
			else setSelectIndex(0);
			return;
		}

		const opts = getSelectOptions(q);
		if (opts.length === 0) {
			setMode("text");
			return;
		}

		setMode("select");
		setSelectIndex(0);
		if (typeof existing === "string") {
			const exactIdx = opts.indexOf(existing);
			if (exactIdx >= 0) {
				setSelectIndex(exactIdx);
				return;
			}
			const otherIdx = opts.findIndex((o) => isOtherOption(o));
			if (otherIdx >= 0 && existing.trim()) {
				setMode("otherText");
				setSelectIndex(otherIdx);
				return;
			}
		}
	}

	// ── Navigation ────────────────────────────────────────────────

	function advance() {
		if (currentIndex() >= questions().length - 1) {
			// Last question — complete
			finish(false);
			return;
		}
		const nextIndex = currentIndex() + 1;
		setCurrentIndex(nextIndex);
		loadQuestionState(questions()[nextIndex] ?? null);
	}

	function setAnswer(id: string, value: AnswerValue) {
		setAnswers((prev) => ({ ...prev, [id]: value }));
	}

	function selectOption(): boolean {
		const q = currentQuestion();
		if (!q) return false;

		if (q.kind === "boolean") {
			const opts = getSelectOptions(q);
			const choice = opts[getValidSelectIndex(q)];
			if (!choice) return false;
			if (choice === "Skip") setAnswer(q.id, "");
			else setAnswer(q.id, choice === "Yes");
			advance();
			return true;
		}

		if (q.kind === "select") {
			const opts = getSelectOptions(q);
			const choice = opts[getValidSelectIndex(q)];
			if (!choice) return false;

			if (choice === "Skip") {
				setAnswer(q.id, "");
				advance();
				return true;
			}

			if (isOtherOption(choice)) {
				setMode("otherText");
				return true;
			}

			setAnswer(q.id, choice);
			advance();
			return true;
		}

		if (q.kind === "multiselect") {
			const opts = getSelectOptions(q);
			const choice = opts[getValidSelectIndex(q)];
			if (!choice) return false;
			const current = getMultiSelectValues(q.id);
			if (choice === "Skip") {
				setAnswer(q.id, []);
				advance();
				return true;
			}
			if (current.includes(choice)) {
				setAnswer(
					q.id,
					current.filter((value) => value !== choice),
				);
			} else {
				setAnswer(q.id, [...current, choice]);
			}
			return true;
		}

		return false;
	}

	function submitText(text: string) {
		const q = currentQuestion();
		if (!q) return;

		const trimmed = text.trim();
		if (!trimmed && q.required !== false) return; // required

		if (mode() === "otherText") {
			setAnswer(q.id, trimmed || "Other");
		} else {
			setAnswer(q.id, trimmed);
		}
		advance();
	}

	function escapeTextMode() {
		if (mode() === "otherText") {
			setMode("select");
		}
	}

	function movePrev() {
		if (mode() === "otherText") {
			setMode("select");
			return;
		}
		if (currentIndex() > 0) {
			const previousIndex = currentIndex() - 1;
			setCurrentIndex(previousIndex);
			loadQuestionState(questions()[previousIndex] ?? null);
		}
	}

	function moveSelectUp() {
		const q = currentQuestion();
		if (!q) return;
		const opts = getSelectOptions(q);
		const index = getValidSelectIndex(q);
		if (opts.length > 0 && index > 0) {
			setSelectIndex(index - 1);
		}
	}

	function moveSelectDown() {
		const q = currentQuestion();
		if (!q) return;
		const opts = getSelectOptions(q);
		const index = getValidSelectIndex(q);
		if (opts.length > 0 && index < opts.length - 1) {
			setSelectIndex(index + 1);
		}
	}

	function submitMultiSelect(): boolean {
		const q = currentQuestion();
		if (!q || q.kind !== "multiselect") return false;
		const selected = getMultiSelectValues(q.id);
		if (selected.length === 0 && q.required !== false) return false;
		advance();
		return true;
	}

	function isOptionSelected(option: string): boolean {
		const q = currentQuestion();
		if (!q || q.kind !== "multiselect") return false;
		return getMultiSelectValues(q.id).includes(option);
	}

	// ── Lifecycle ─────────────────────────────────────────────────

	function notifyActiveChanged(): void {
		const value = active();
		for (const listener of listeners) listener(value);
	}

	function subscribe(listener: (active: boolean) => void): () => void {
		listeners.add(listener);
		return () => listeners.delete(listener);
	}

	function activate(
		params: GuidedQuestionsInput,
	): Promise<GuidedQuestionsResult> {
		const qs = (params.questions || []).map(normalizeQuestion);
		if (qs.length === 0) {
			return Promise.resolve({ cancelled: true, answers: {} });
		}

		setQuestions(qs);
		setTitle(params.title?.trim() || "Guided questionnaire");
		setIntro(params.intro?.trim() || "");
		setCurrentIndex(0);
		setSelectIndex(0);
		setAnswers({});
		setActive(true);
		notifyActiveChanged();
		loadQuestionState(qs[0] ?? null);

		return new Promise<GuidedQuestionsResult>((resolve) => {
			resolveGuidedQuestions = resolve;
		});
	}

	function finish(cancelled: boolean) {
		const result: GuidedQuestionsResult = {
			cancelled,
			answers: answers(),
		};
		setActive(false);
		notifyActiveChanged();
		resolveGuidedQuestions?.(result);
		resolveGuidedQuestions = null;
	}

	function cancel() {
		finish(true);
	}

	return {
		get active() {
			return active();
		},
		get title() {
			return title();
		},
		get intro() {
			return intro();
		},
		get questions() {
			return questions();
		},
		get currentIndex() {
			return currentIndex();
		},
		get currentQuestion() {
			return currentQuestion();
		},
		get selectIndex() {
			return selectIndex();
		},
		get mode() {
			return mode();
		},
		get answers() {
			return answers();
		},
		get answeredCount() {
			return answeredCount();
		},
		getSelectOptions,
		subscribe,
		activate,
		cancel,
		selectOption,
		submitText,
		submitMultiSelect,
		escapeTextMode,
		movePrev,
		moveSelectUp,
		moveSelectDown,
		isOptionSelected,
		getValidSelectIndex,
	};
}

export type GuidedQuestionsController = ReturnType<
	typeof createGuidedQuestionsController
>;
