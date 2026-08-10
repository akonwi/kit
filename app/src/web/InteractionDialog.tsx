/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	For,
	type JSX,
	Match,
	Show,
	Switch,
} from "solid-js";
import { isRecord } from "./client-state";
import { DialogFrame } from "./DialogFrame";
import { SafeMarkdown } from "./SafeMarkdown";
import { useWebClient } from "./WebClientContext";

function requestPayload(request: Record<string, unknown>) {
	return isRecord(request.payload) ? request.payload : {};
}

function responseForRequest(
	request: Record<string, unknown>,
	form: HTMLFormElement,
	cancelled: boolean,
): unknown {
	const payload = requestPayload(request);
	const data = new FormData(form);
	if (request.kind === "confirm") return { confirmed: !cancelled };
	if (request.kind === "input") {
		return { value: cancelled ? null : String(data.get("value") ?? "") };
	}
	if (request.kind === "select") {
		return {
			optionId: cancelled ? null : (data.get("option")?.toString() ?? null),
		};
	}
	if (request.kind === "guided_questions") {
		if (cancelled) return { cancelled: true, answers: {} };
		const answers: Record<string, unknown> = {};
		for (const question of Array.isArray(payload.questions)
			? payload.questions.filter(isRecord)
			: []) {
			if (
				typeof question.id !== "string" ||
				typeof question.kind !== "string"
			) {
				continue;
			}
			if (question.kind === "multiselect") {
				answers[question.id] = data
					.getAll(question.id)
					.map((value) => String(value));
			} else if (question.kind === "boolean") {
				const value = data.get(question.id);
				if (value !== null) answers[question.id] = value === "true";
			} else {
				answers[question.id] = String(data.get(question.id) ?? "");
			}
		}
		return { cancelled: false, answers };
	}
	return {};
}

function GuidedQuestions(props: {
	payload: Record<string, unknown>;
}): JSX.Element {
	const questions = () =>
		Array.isArray(props.payload.questions)
			? props.payload.questions.filter(isRecord)
			: [];
	return (
		<For each={questions()}>
			{(question) => {
				const id =
					typeof question.id === "string" ? question.id : "invalid-question";
				const kind = typeof question.kind === "string" ? question.kind : "text";
				const options =
					kind === "boolean"
						? ["true", "false"]
						: Array.isArray(question.options)
							? question.options.filter(
									(option): option is string => typeof option === "string",
								)
							: [];
				return (
					<fieldset
						data-question-kind={kind}
						data-required={String(question.required === true)}
					>
						<legend>
							{typeof question.label === "string" ? question.label : id}
						</legend>
						<Show when={typeof question.help === "string"}>
							<small>{question.help as string}</small>
						</Show>
						<Switch>
							<Match when={kind === "text"}>
								<input name={id} required={question.required === true} />
							</Match>
							<Match when>
								<For each={options}>
									{(option) => (
										<label class="interaction-option">
											<input
												type={kind === "multiselect" ? "checkbox" : "radio"}
												name={id}
												value={option}
												required={
													question.required === true && kind !== "multiselect"
												}
											/>
											{option}
										</label>
									)}
								</For>
							</Match>
						</Switch>
					</fieldset>
				);
			}}
		</For>
	);
}

function InteractionFields(props: {
	request: Record<string, unknown>;
}): JSX.Element {
	const payload = requestPayload(props.request);
	return (
		<Switch>
			<Match when={props.request.kind === "input"}>
				<label class="interaction-question">
					<span>Response</span>
					<input
						name="value"
						value={
							typeof payload.initialValue === "string"
								? payload.initialValue
								: ""
						}
						placeholder={
							typeof payload.placeholder === "string" ? payload.placeholder : ""
						}
					/>
				</label>
			</Match>
			<Match when={props.request.kind === "select"}>
				<For
					each={
						Array.isArray(payload.options)
							? payload.options.filter(isRecord)
							: []
					}
				>
					{(option) => (
						<label class="interaction-option">
							<input
								type="radio"
								name="option"
								value={typeof option.id === "string" ? option.id : ""}
								required
							/>
							<span>
								{typeof option.label === "string" ? option.label : "Option"}
							</span>
							<Show when={typeof option.description === "string"}>
								<small>{option.description as string}</small>
							</Show>
						</label>
					)}
				</For>
			</Match>
			<Match when={props.request.kind === "guided_questions"}>
				<GuidedQuestions payload={payload} />
			</Match>
		</Switch>
	);
}

function InteractionContent(props: {
	request: Record<string, unknown>;
	form: () => HTMLFormElement | undefined;
}): JSX.Element {
	const { snapshot, controller } = useWebClient();
	const payload = requestPayload(props.request);
	const requestId = props.request.id as string;
	const hydrationError = createMemo(() =>
		snapshot().interactionHydrationErrors.get(requestId),
	);
	const responseError = createMemo(() =>
		snapshot().interactionResponseErrors.get(requestId),
	);
	const disabled = createMemo(
		() =>
			snapshot().protocol.phase !== "live" ||
			snapshot().answeringInteractionId === requestId,
	);

	createEffect(() => {
		if (
			props.request.payloadOmitted === true &&
			snapshot().protocol.phase === "live" &&
			!hydrationError()
		) {
			controller.ensureInteractionHydrated(requestId);
		}
	});

	const answer = (cancelled: boolean) => {
		const form = props.form();
		if (!form) return;
		void controller.answerInteraction(
			requestId,
			responseForRequest(props.request, form, cancelled),
		);
	};

	return (
		<m-vstack gap="md">
			<header class="interaction-header">
				<div class="interaction-title-row">
					<h2 id="interaction-title">
						{typeof payload.title === "string"
							? payload.title
							: "Kit needs your input"}
					</h2>
					<Show when={snapshot().protocol.pendingInteractions.length > 1}>
						<small>1 of {snapshot().protocol.pendingInteractions.length}</small>
					</Show>
				</div>
			</header>
			<fieldset class="interaction-frame" disabled={disabled()}>
				<div class="interaction-layout">
					<div class="interaction-body">
						<Show
							when={
								props.request.payloadOmitted === true ||
								typeof payload.message === "string"
							}
						>
							<SafeMarkdown
								id="interaction-message"
								class="interaction-message"
								profile="interaction"
								content={
									props.request.payloadOmitted === true
										? (hydrationError() ?? "Loading interaction…")
										: typeof payload.message === "string"
											? payload.message
											: ""
								}
							/>
						</Show>
						<Show when={responseError()}>
							<p class="interaction-error" role="alert">
								{responseError()}
							</p>
						</Show>
						<div class="interaction-fields">
							<Show when={props.request.payloadOmitted !== true}>
								<InteractionFields request={props.request} />
							</Show>
						</div>
					</div>
					<m-hstack class="interaction-actions" gap="sm" justify="end">
						<button
							type="button"
							data-interaction-action="cancel"
							data-variant="ghost"
							onClick={() => answer(true)}
						>
							{typeof payload.cancelLabel === "string"
								? payload.cancelLabel
								: "Cancel"}
						</button>
						<Show when={hydrationError()}>
							<button
								type="button"
								data-variant="primary"
								onClick={() => controller.retryInteraction(requestId)}
							>
								Retry
							</button>
						</Show>
						<Show when={props.request.payloadOmitted !== true}>
							<button
								type="submit"
								data-interaction-action="confirm"
								data-variant="primary"
							>
								{typeof payload.confirmLabel === "string"
									? payload.confirmLabel
									: props.request.kind === "confirm"
										? "Confirm"
										: "Submit"}
							</button>
						</Show>
					</m-hstack>
				</div>
			</fieldset>
		</m-vstack>
	);
}

export function InteractionDialog(): JSX.Element {
	const { snapshot, controller } = useWebClient();
	let form: HTMLFormElement | undefined;
	const protocol = createMemo(() => snapshot().protocol);
	const request = createMemo(() =>
		protocol().pendingInteractions.find(isRecord),
	);
	const requestKey = createMemo(() => {
		const active = request();
		return active && typeof active.id === "string"
			? `${active.id}:${active.payloadOmitted === true ? "omitted" : "ready"}`
			: null;
	});
	const requestDescriptionId = createMemo(() => {
		const active = request();
		if (!active) return undefined;
		return active.payloadOmitted === true ||
			typeof requestPayload(active).message === "string"
			? "interaction-message"
			: undefined;
	});

	const validateMultiselects = () => {
		if (!form) return false;
		for (const field of Array.from(
			form.querySelectorAll<HTMLFieldSetElement>(
				'fieldset[data-question-kind="multiselect"]',
			),
		)) {
			if (field.dataset.required !== "true") continue;
			const inputs = Array.from(
				field.querySelectorAll<HTMLInputElement>('input[type="checkbox"]'),
			);
			const first = inputs[0];
			if (!first) continue;
			first.setCustomValidity(
				inputs.some((input) => input.checked)
					? ""
					: "Select at least one option",
			);
		}
		return form.reportValidity();
	};

	return (
		<DialogFrame
			open={requestKey() !== null}
			focusKey={requestKey()}
			id="interaction-dialog"
			labelledBy="interaction-title"
			describedBy={requestDescriptionId()}
			onAfterOpen={() => {
				const active = request();
				if (active?.kind === "confirm") {
					const payload = requestPayload(active);
					const action = payload.defaultValue === true ? "confirm" : "cancel";
					form
						?.querySelector<HTMLElement>(
							`button[data-interaction-action="${action}"]`,
						)
						?.focus();
					return;
				}
				const field = form?.querySelector<HTMLElement>(
					".interaction-fields input, .interaction-fields select, .interaction-fields textarea",
				);
				const submit = form?.querySelector<HTMLElement>(
					'button[type="submit"]',
				);
				const fallback = form?.querySelector<HTMLElement>("button");
				(field ?? submit ?? fallback)?.focus();
			}}
			onCancel={() => {
				const active = request();
				if (!active || typeof active.id !== "string" || !form) return;
				void controller.answerInteraction(
					active.id,
					responseForRequest(active, form, true),
				);
			}}
		>
			<form
				ref={form}
				method="dialog"
				onSubmit={(event) => {
					event.preventDefault();
					const active = request();
					if (
						!active ||
						typeof active.id !== "string" ||
						snapshot().protocol.phase !== "live" ||
						!form ||
						!validateMultiselects()
					) {
						return;
					}
					void controller.answerInteraction(
						active.id,
						responseForRequest(active, form, false),
					);
				}}
				onChange={(event) => {
					if (!(event.target instanceof HTMLInputElement)) return;
					for (const input of Array.from(
						event.target.form?.elements.namedItem(event.target.name) instanceof
							RadioNodeList
							? Array.from(
									event.target.form.elements.namedItem(
										event.target.name,
									) as RadioNodeList,
								).filter(
									(item): item is HTMLInputElement =>
										item instanceof HTMLInputElement,
								)
							: [event.target],
					)) {
						input.setCustomValidity("");
					}
				}}
			>
				{/* The key alone owns form rebuilds; do not track request object identity here. */}
				<Show when={requestKey()} keyed>
					{(_key) => {
						const active = request();
						return active ? (
							<InteractionContent request={active} form={() => form} />
						) : null;
					}}
				</Show>
			</form>
		</DialogFrame>
	);
}
