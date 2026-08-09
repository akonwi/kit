/** @jsxImportSource solid-js */
import { createMemo, For, type JSX, onCleanup, onMount } from "solid-js";
import { useWebClient } from "./WebClientContext";

export function Composer(): JSX.Element {
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
	const queueStatus = createMemo(() =>
		protocol().queuedMessageCount > 0
			? `${protocol().queuedMessageCount} queued`
			: streaming()
				? "Send queues a follow-up"
				: "",
	);

	const submit = async (event: SubmitEvent) => {
		event.preventDefault();
		if (!input) return;
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
		<footer class="composer-dock">
			<div class="attachment-list">
				<For each={snapshot().attachments}>
					{(attachment) => (
						<span class="attachment-chip">
							{attachment.file.name}
							<button
								type="button"
								data-variant="ghost"
								data-size="small"
								disabled={snapshot().submitting}
								onClick={() => void controller.removeAttachment(attachment)}
							>
								Remove
							</button>
						</span>
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
					placeholder="Ask Kit…"
					onKeyDown={(event) => {
						if (event.key !== "Enter" || event.shiftKey || event.isComposing) {
							return;
						}
						event.preventDefault();
						if (enabled()) event.currentTarget.form?.requestSubmit();
						else controller.reportComposerUnavailable();
					}}
				/>
				<m-hstack class="composer-actions" gap="xs" align="center">
					<button
						type="button"
						data-variant="ghost"
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
					<span class="queue-status">{queueStatus()}</span>
					<span class="composer-spacer" />
					<button
						type="button"
						data-variant="danger"
						hidden={!streaming()}
						disabled={!enabled()}
						onClick={() => void controller.abort()}
					>
						Abort
					</button>
					<button type="submit" data-variant="primary" disabled={!enabled()}>
						{snapshot().submitting ? "Sending…" : "Send"}
					</button>
				</m-hstack>
			</form>
			<p
				class="app-status"
				role="status"
				aria-live="polite"
				data-error={String(snapshot().status.isError)}
			>
				{snapshot().status.message}
			</p>
		</footer>
	);
}
