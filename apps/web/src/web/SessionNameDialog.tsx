/** @jsxImportSource solid-js */
import { createEffect, createSignal, type JSX, Show } from "solid-js";
import { DialogFrame } from "./DialogFrame";
import { OverlayHintBar } from "./OverlayHintBar";

export function SessionNameDialog(props: {
	open: boolean;
	currentName: string;
	onCancel: () => void;
	onSubmit: (name: string) => Promise<boolean>;
}): JSX.Element {
	let input: HTMLInputElement | undefined;
	let wasOpen = false;
	const [name, setName] = createSignal("");
	const [submitting, setSubmitting] = createSignal(false);
	const [error, setError] = createSignal("");

	createEffect(() => {
		if (props.open && !wasOpen) {
			setName(props.currentName);
			setSubmitting(false);
			setError("");
		}
		wasOpen = props.open;
	});

	function cancel(): void {
		if (!submitting()) props.onCancel();
	}

	async function submit(): Promise<void> {
		if (submitting()) return;
		const nextName = name().trim();
		if (!nextName) {
			setError("Session name is required");
			input?.focus();
			return;
		}
		if (nextName === props.currentName.trim()) {
			props.onCancel();
			return;
		}
		setSubmitting(true);
		setError("");
		try {
			if (await props.onSubmit(nextName)) props.onCancel();
		} finally {
			setSubmitting(false);
		}
	}

	return (
		<DialogFrame
			open={props.open}
			id="session-name-dialog"
			class="input-dialog"
			labelledBy="session-name-dialog-title"
			onAfterOpen={() => {
				input?.focus();
				input?.select();
			}}
			onCancel={cancel}
		>
			<form
				onSubmit={(event) => {
					event.preventDefault();
					void submit();
				}}
			>
				<header>
					<h2 id="session-name-dialog-title">Session name</h2>
				</header>
				<div class="input-dialog-body">
					<label>
						<span data-visually-hidden>Session name</span>
						<input
							ref={input}
							type="text"
							value={name()}
							disabled={submitting()}
							autocomplete="off"
							onInput={(event) => {
								setName(event.currentTarget.value);
								setError("");
							}}
						/>
					</label>
					<Show when={error()}>
						{(message) => (
							<p class="input-dialog-error" role="alert">
								{message()}
							</p>
						)}
					</Show>
				</div>
				<OverlayHintBar
					class="input-dialog-footer"
					hints={["Enter save", "Esc close"]}
				/>
			</form>
		</DialogFrame>
	);
}
