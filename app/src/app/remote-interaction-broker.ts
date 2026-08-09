import { randomUUID } from "node:crypto";
import type {
	AnswerValue,
	GuidedQuestion,
	GuidedQuestionsInput,
	GuidedQuestionsRequester,
	GuidedQuestionsResult,
} from "../features/guided-questions/types";
import { normalizeQuestions } from "../features/guided-questions/types";

export const REMOTE_INTERACTION_KINDS = [
	"confirm",
	"input",
	"select",
	"guided_questions",
] as const;

export type RemoteInteractionKind = (typeof REMOTE_INTERACTION_KINDS)[number];

export type RemoteInteractionRequest = {
	id: string;
	kind: RemoteInteractionKind;
	createdAt: string;
	payload: Record<string, unknown>;
};

export type RemoteInteractionEvent =
	| {
			type: "ui_snapshot";
			generation: number;
			requests: RemoteInteractionRequest[];
	  }
	| {
			type: "ui_request";
			generation: number;
			request: RemoteInteractionRequest;
	  }
	| {
			type: "ui_resolved";
			generation: number;
			requestId: string;
			kind: RemoteInteractionKind;
			resolution: "answered" | "aborted" | "shutdown";
			response: unknown;
	  };

export type RemoteInteractionResponseResult =
	| { accepted: true }
	| { accepted: false; error: string };

export type RemoteConfirmInput = {
	title: string;
	message?: string;
	confirmLabel?: string;
	cancelLabel?: string;
	defaultValue?: boolean;
	signal?: AbortSignal;
};

export type RemoteInputInput = {
	title: string;
	message?: string;
	placeholder?: string;
	initialValue?: string;
	signal?: AbortSignal;
};

export type RemoteSelectInput<T> = {
	title: string;
	message?: string;
	options: string[] | Array<{ label: string; value: T; description?: string }>;
	filterable?: boolean;
	placeholder?: string;
	signal?: AbortSignal;
};

type ValidationResult<T> =
	| { valid: true; value: T; publicResponse: unknown }
	| { valid: false; error: string };

type PendingInteraction = {
	request: RemoteInteractionRequest;
	fallback: unknown;
	fallbackResponse: unknown;
	validate(response: unknown): ValidationResult<unknown>;
	resolve(value: unknown): void;
	signal?: AbortSignal;
	abortListener?: () => void;
};

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function validateGuidedAnswer(
	question: GuidedQuestion,
	value: unknown,
): ValidationResult<AnswerValue | undefined> {
	if (value === undefined) {
		return question.required
			? { valid: false, error: `Missing required answer: ${question.id}` }
			: { valid: true, value: undefined, publicResponse: undefined };
	}

	switch (question.kind) {
		case "boolean":
			if (typeof value === "boolean" || (!question.required && value === "")) {
				return { valid: true, value, publicResponse: value };
			}
			return {
				valid: false,
				error: `Answer ${question.id} must be a boolean`,
			};
		case "multiselect": {
			if (
				!Array.isArray(value) ||
				!value.every((item) => typeof item === "string")
			) {
				return {
					valid: false,
					error: `Answer ${question.id} must be a string array`,
				};
			}
			if (question.required && value.length === 0) {
				return {
					valid: false,
					error: `Answer ${question.id} requires at least one selection`,
				};
			}
			const options = new Set(question.options ?? []);
			if (value.some((item) => !options.has(item))) {
				return {
					valid: false,
					error: `Answer ${question.id} contains an unknown option`,
				};
			}
			return { valid: true, value, publicResponse: value };
		}
		case "select": {
			if (typeof value !== "string") {
				return {
					valid: false,
					error: `Answer ${question.id} must be a string`,
				};
			}
			if (!question.required && value === "") {
				return { valid: true, value, publicResponse: value };
			}
			if (!(question.options ?? []).includes(value)) {
				return {
					valid: false,
					error: `Answer ${question.id} contains an unknown option`,
				};
			}
			return { valid: true, value, publicResponse: value };
		}
		case "text":
			if (typeof value !== "string") {
				return {
					valid: false,
					error: `Answer ${question.id} must be a string`,
				};
			}
			if (question.required && !value.trim()) {
				return {
					valid: false,
					error: `Answer ${question.id} must not be empty`,
				};
			}
			return { valid: true, value, publicResponse: value };
	}
}

export class RemoteInteractionBroker implements GuidedQuestionsRequester {
	private readonly pending = new Map<string, PendingInteraction>();
	private generation = 0;
	private readonly listeners = new Set<
		(event: RemoteInteractionEvent) => void
	>();
	private disposed = false;

	subscribe(listener: (event: RemoteInteractionEvent) => void): () => void {
		if (this.disposed) return () => {};
		this.listeners.add(listener);
		return () => this.listeners.delete(listener);
	}

	connectClient(): RemoteInteractionEvent[] {
		if (this.disposed) return [];
		const snapshot = this.getPendingSnapshot();
		return [
			{
				type: "ui_snapshot",
				generation: snapshot.generation,
				requests: snapshot.requests,
			},
		];
	}

	getPendingSnapshot(): {
		generation: number;
		requests: RemoteInteractionRequest[];
	} {
		return {
			generation: this.generation,
			requests: [...this.pending.values()].map((pending) => pending.request),
		};
	}

	getPendingRequests(): RemoteInteractionRequest[] {
		return this.getPendingSnapshot().requests;
	}

	confirm(input: RemoteConfirmInput): Promise<boolean> {
		const { signal, ...payload } = input;
		return this.request(
			"confirm",
			payload,
			false,
			{ confirmed: false },
			(response) => {
				if (!isRecord(response) || typeof response.confirmed !== "boolean") {
					return {
						valid: false,
						error: "Confirm response must contain a boolean confirmed value",
					};
				}
				return {
					valid: true,
					value: response.confirmed,
					publicResponse: { confirmed: response.confirmed },
				};
			},
			signal,
		);
	}

	input(input: RemoteInputInput): Promise<string | undefined> {
		const { signal, ...payload } = input;
		return this.request(
			"input",
			payload,
			undefined,
			{ value: null },
			(response) => {
				if (
					!isRecord(response) ||
					(response.value !== null && typeof response.value !== "string")
				) {
					return {
						valid: false,
						error: "Input response must contain a string or null value",
					};
				}
				return {
					valid: true,
					value: response.value ?? undefined,
					publicResponse: { value: response.value },
				};
			},
			signal,
		);
	}

	select(
		input: Omit<RemoteSelectInput<string>, "options"> & { options: string[] },
	): Promise<string | undefined>;
	select<T>(
		input: Omit<RemoteSelectInput<T>, "options"> & {
			options: Array<{ label: string; value: T; description?: string }>;
		},
	): Promise<T | undefined>;
	select<T>(input: RemoteSelectInput<T>): Promise<T | string | undefined> {
		const { signal, options, ...rest } = input;
		const normalizedOptions = options.map((option, index) =>
			typeof option === "string"
				? { id: String(index), label: option }
				: {
						id: String(index),
						label: option.label,
						description: option.description,
					},
		);
		return this.request<T | string | undefined>(
			"select",
			{ ...rest, options: normalizedOptions },
			undefined,
			{ optionId: null },
			(response) => {
				if (
					!isRecord(response) ||
					(response.optionId !== null && typeof response.optionId !== "string")
				) {
					return {
						valid: false,
						error: "Select response must contain a string or null optionId",
					};
				}
				if (response.optionId === null) {
					return {
						valid: true,
						value: undefined,
						publicResponse: { optionId: null },
					};
				}
				const index = Number(response.optionId);
				if (
					!Number.isInteger(index) ||
					index < 0 ||
					index >= options.length ||
					String(index) !== response.optionId
				) {
					return { valid: false, error: "Selected option does not exist" };
				}
				const selected = options[index];
				return {
					valid: true,
					value: typeof selected === "string" ? selected : selected?.value,
					publicResponse: { optionId: response.optionId },
				};
			},
			signal,
		);
	}

	activate(
		params: GuidedQuestionsInput,
		signal?: AbortSignal,
	): Promise<GuidedQuestionsResult> {
		const normalizedParams = {
			...params,
			questions: normalizeQuestions(params.questions),
		};
		const fallback: GuidedQuestionsResult = { cancelled: true, answers: {} };
		return this.request(
			"guided_questions",
			normalizedParams as unknown as Record<string, unknown>,
			fallback,
			fallback,
			(response) =>
				this.validateGuidedQuestionsResponse(normalizedParams, response),
			signal,
		);
	}

	respond(
		requestId: string,
		response: unknown,
	): RemoteInteractionResponseResult {
		const pending = this.pending.get(requestId);
		if (!pending) {
			return { accepted: false, error: "Interaction is no longer pending" };
		}
		const validated = pending.validate(response);
		if (!validated.valid) {
			return { accepted: false, error: validated.error };
		}
		this.settle(pending, "answered", validated.value, validated.publicResponse);
		return { accepted: true };
	}

	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		for (const pending of [...this.pending.values()]) {
			this.settle(
				pending,
				"shutdown",
				pending.fallback,
				pending.fallbackResponse,
			);
		}
		this.listeners.clear();
	}

	private request<T>(
		kind: RemoteInteractionKind,
		payload: Record<string, unknown>,
		fallback: T,
		fallbackResponse: unknown,
		validate: (response: unknown) => ValidationResult<T>,
		signal?: AbortSignal,
	): Promise<T> {
		if (this.disposed || signal?.aborted) return Promise.resolve(fallback);
		const request: RemoteInteractionRequest = {
			id: randomUUID(),
			kind,
			createdAt: new Date().toISOString(),
			payload,
		};
		return new Promise<T>((resolve) => {
			const pending: PendingInteraction = {
				request,
				fallback,
				fallbackResponse,
				validate: (response) => validate(response),
				resolve: (value) => resolve(value as T),
				signal,
			};
			if (signal) {
				pending.abortListener = () => {
					this.settle(pending, "aborted", fallback, fallbackResponse);
				};
				signal.addEventListener("abort", pending.abortListener, { once: true });
			}
			this.pending.set(request.id, pending);
			this.generation += 1;
			this.publish({
				type: "ui_request",
				generation: this.generation,
				request,
			});
		});
	}

	private validateGuidedQuestionsResponse(
		params: GuidedQuestionsInput,
		response: unknown,
	): ValidationResult<GuidedQuestionsResult> {
		if (
			!isRecord(response) ||
			typeof response.cancelled !== "boolean" ||
			!isRecord(response.answers)
		) {
			return {
				valid: false,
				error:
					"Guided questions response requires cancelled and answers fields",
			};
		}
		const answers = Object.create(null) as Record<string, AnswerValue>;
		for (const question of params.questions) {
			const value = Object.hasOwn(response.answers, question.id)
				? response.answers[question.id]
				: undefined;
			const validated = validateGuidedAnswer(question, value);
			if (!validated.valid) {
				if (response.cancelled) continue;
				return validated;
			}
			if (validated.value !== undefined) {
				answers[question.id] = validated.value;
			}
		}
		const result = { cancelled: response.cancelled, answers };
		return { valid: true, value: result, publicResponse: result };
	}

	private settle(
		pending: PendingInteraction,
		resolution: "answered" | "aborted" | "shutdown",
		value: unknown,
		publicResponse: unknown,
	): void {
		if (!this.pending.delete(pending.request.id)) return;
		this.generation += 1;
		if (pending.signal && pending.abortListener) {
			pending.signal.removeEventListener("abort", pending.abortListener);
		}
		pending.resolve(value);
		this.publish({
			type: "ui_resolved",
			generation: this.generation,
			requestId: pending.request.id,
			kind: pending.request.kind,
			resolution,
			response: publicResponse,
		});
	}

	private publish(event: RemoteInteractionEvent): void {
		for (const listener of [...this.listeners]) {
			try {
				listener(event);
			} catch (error) {
				console.error(
					`Remote interaction listener failed: ${error instanceof Error ? error.message : String(error)}`,
				);
			}
		}
	}
}
