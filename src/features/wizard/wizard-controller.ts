/**
 * Wizard controller — manages question navigation, answer state,
 * and input mode for the guided questionnaire.
 */

import { createMemo, createSignal } from "solid-js";
import {
	type AnswerValue,
	normalizeQuestion,
	type WizardInput,
	type WizardQuestion,
} from "./types";

export type WizardMode = "select" | "text" | "otherText";

export type WizardResult = {
	cancelled: boolean;
	answers: Record<string, AnswerValue>;
};

export function createWizardController() {
	const [active, setActive] = createSignal(false);
	const [questions, setQuestions] = createSignal<WizardQuestion[]>([]);
	const [title, setTitle] = createSignal("");
	const [intro, setIntro] = createSignal("");
	const [currentIndex, setCurrentIndex] = createSignal(0);
	const [selectIndex, setSelectIndex] = createSignal(0);
	const [mode, setMode] = createSignal<WizardMode>("select");
	const [answers, setAnswers] = createSignal<Record<string, AnswerValue>>({});

	let resolveWizard: ((result: WizardResult) => void) | null = null;

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
			return typeof v === "string" && v.trim().length > 0;
		}).length;
	});

	// ── Option helpers ────────────────────────────────────────────

	function getSelectOptions(q: WizardQuestion): string[] {
		if (q.kind === "boolean") {
			return q.required === false ? ["Yes", "No", "Skip"] : ["Yes", "No"];
		}
		if (q.kind === "select") {
			const provided = Array.isArray(q.options)
				? q.options.filter(Boolean)
				: [];
			return q.required === false ? [...provided, "Skip"] : provided;
		}
		return [];
	}

	function isOtherOption(value: string): boolean {
		return /^other(\b|\s|:)/i.test(value);
	}

	// ── State loading ─────────────────────────────────────────────

	function loadQuestionState() {
		const q = currentQuestion();
		if (!q) return;

		const existing = answers()[q.id];

		if (q.kind === "text") {
			setMode("text");
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
		setCurrentIndex(currentIndex() + 1);
		loadQuestionState();
	}

	function setAnswer(id: string, value: AnswerValue) {
		setAnswers((prev) => ({ ...prev, [id]: value }));
	}

	function selectOption(): boolean {
		const q = currentQuestion();
		if (!q) return false;

		if (q.kind === "boolean") {
			const opts = getSelectOptions(q);
			const choice = opts[selectIndex()];
			if (!choice) return false;
			if (choice === "Skip") setAnswer(q.id, "");
			else setAnswer(q.id, choice === "Yes");
			advance();
			return true;
		}

		if (q.kind === "select") {
			const opts = getSelectOptions(q);
			const choice = opts[selectIndex()];
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
			setCurrentIndex(currentIndex() - 1);
			loadQuestionState();
		}
	}

	function moveSelectUp() {
		const q = currentQuestion();
		if (!q) return;
		const opts = getSelectOptions(q);
		if (opts.length > 0 && selectIndex() > 0) {
			setSelectIndex(selectIndex() - 1);
		}
	}

	function moveSelectDown() {
		const q = currentQuestion();
		if (!q) return;
		const opts = getSelectOptions(q);
		if (opts.length > 0 && selectIndex() < opts.length - 1) {
			setSelectIndex(selectIndex() + 1);
		}
	}

	// ── Lifecycle ─────────────────────────────────────────────────

	function activate(params: WizardInput): Promise<WizardResult> {
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
		loadQuestionState();

		return new Promise<WizardResult>((resolve) => {
			resolveWizard = resolve;
		});
	}

	function finish(cancelled: boolean) {
		const result: WizardResult = { cancelled, answers: answers() };
		setActive(false);
		resolveWizard?.(result);
		resolveWizard = null;
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
		activate,
		cancel,
		selectOption,
		submitText,
		escapeTextMode,
		movePrev,
		moveSelectUp,
		moveSelectDown,
	};
}

export type WizardController = ReturnType<typeof createWizardController>;
