/** @jsxImportSource solid-js */
import { createMemo, For, type JSX, onCleanup, onMount } from "solid-js";
import { useWebClient } from "./WebClientContext";

export function ComposerDock(): JSX.Element {
	const { snapshot, controller, registerComposerFocus } = useWebClient();
	let input: HTMLTextAreaElement | undefined;
	let attachmentInput: HTMLInputElement | undefined;
	const protocol = createMemo(() => snapshot().protocol);
	const enabled = createMemo(
		() => protocol().phase === "live" && !snapshot().submitting,
	);
	const streaming = createMemo(
		() => protocol().serverState.isStreaming === true,
	);

	const submit = async (event: SubmitEvent) => {
		event.preventDefault();
		if (!input || !enabled()) return;
		const submittedValue = input.value;
		if (
			(await controller.submit(submittedValue)) &&
			input.value === submittedValue
		) {
			input.value = "";
		}
	};

	let unregisterComposerFocus: (() => void) | undefined;
	onMount(() => {
		unregisterComposerFocus = registerComposerFocus(() => {
			if (!input) return false;
			input.focus();
			return document.activeElement === input;
		});
	});
	onCleanup(() => unregisterComposerFocus?.());

	return (
		<section class="composer-dock" aria-label="Message composer">
			<div class="attachment-list">
				<For each={snapshot().attachments}>
					{(attachment) => (
						<div class="attachment-row">
							<span class="attachment-name" title={attachment.file.name}>
								{attachment.file.name}
							</span>
							<button
								class="attachment-remove"
								type="button"
								data-variant="ghost"
								data-size="small"
								disabled={snapshot().submitting}
								aria-label={`Remove ${attachment.file.name}`}
								onClick={() => void controller.removeAttachment(attachment)}
							>
								×
							</button>
						</div>
					)}
				</For>
			</div>
			<form class="composer-form" onSubmit={submit}>
				<label for="composer-input" data-visually-hidden>
					Message Kit
				</label>
				<textarea
					ref={input}
					id="composer-input"
					name="message"
					rows={1}
					placeholder="Ask kit to do something..."
					onKeyDown={(event) => {
						if (event.key !== "Enter" || event.shiftKey || event.isComposing) {
							return;
						}
						event.preventDefault();
						if (enabled()) event.currentTarget.form?.requestSubmit();
						else controller.reportComposerUnavailable();
					}}
				/>
				<div class="composer-actions">
					<button
						type="button"
						data-variant="ghost"
						data-size="small"
						disabled={!enabled() || streaming()}
						onClick={() => attachmentInput?.click()}
					>
						Attach
					</button>
					<input
						ref={attachmentInput}
						type="file"
						multiple
						hidden
						disabled={!enabled() || streaming()}
						onChange={(event) => {
							controller.addAttachments(
								Array.from(event.currentTarget.files ?? []),
							);
							event.currentTarget.value = "";
						}}
					/>
					<button
						type={streaming() ? "button" : "submit"}
						data-variant={streaming() ? "danger" : "ghost"}
						data-size="small"
						disabled={!enabled()}
						onClick={(event) => {
							if (!streaming()) return;
							event.preventDefault();
							void controller.abort();
						}}
					>
						{streaming()
							? "Abort"
							: snapshot().submitting
								? "Sending…"
								: "Send"}
					</button>
				</div>
			</form>
		</section>
	);
}
