/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	type JSX,
	onCleanup,
	onMount,
} from "solid-js";
import { useCommandPalette } from "./CommandPalette";
import {
	hasComposerPayload,
	shouldSubmitComposerKey,
} from "./composer-keyboard";
import { useWebClient } from "./WebClientContext";
import { WebIcon } from "./WebIcon";

export function ComposerDock(): JSX.Element {
	const { snapshot, controller, registerComposerFocus } = useWebClient();
	const { openPalette } = useCommandPalette();
	let input: HTMLTextAreaElement | undefined;
	let attachmentInput: HTMLInputElement | undefined;
	const [message, setMessage] = createSignal("");
	const [sendCoolingDown, setSendCoolingDown] = createSignal(false);
	const protocol = createMemo(() => snapshot().protocol);
	const enabled = createMemo(
		() => protocol().phase === "live" && !snapshot().submitting,
	);
	const streaming = createMemo(
		() => protocol().serverState.isStreaming === true,
	);
	const hasPayload = createMemo(() =>
		hasComposerPayload(message(), snapshot().attachments.length),
	);
	let wasStreaming = streaming();
	let sendCooldown: ReturnType<typeof setTimeout> | undefined;
	createEffect(() => {
		const isStreaming = streaming();
		if (wasStreaming && !isStreaming) {
			setSendCoolingDown(true);
			clearTimeout(sendCooldown);
			sendCooldown = setTimeout(() => setSendCoolingDown(false), 400);
		}
		wasStreaming = isStreaming;
	});

	const submit = async (event: SubmitEvent) => {
		event.preventDefault();
		if (!input || !enabled()) return;
		const submittedValue = input.value;
		if (
			(await controller.submit(submittedValue)) &&
			input.value === submittedValue
		) {
			input.value = "";
			setMessage("");
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
	onCleanup(() => {
		unregisterComposerFocus?.();
		clearTimeout(sendCooldown);
	});

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
								title={`Remove ${attachment.file.name}`}
								onClick={() => void controller.removeAttachment(attachment)}
							>
								<WebIcon name="close" />
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
					onInput={(event) => setMessage(event.currentTarget.value)}
					onKeyDown={(event) => {
						if (
							!shouldSubmitComposerKey(
								event,
								window.matchMedia("(pointer: coarse)").matches,
							)
						) {
							return;
						}
						event.preventDefault();
						if (enabled()) event.currentTarget.form?.requestSubmit();
						else controller.reportComposerUnavailable();
					}}
				/>
				<div class="composer-actions">
					<button
						class="composer-attach"
						type="button"
						data-variant="ghost"
						data-size="small"
						disabled={!enabled() || streaming()}
						aria-label="Attach files"
						title="Attach files"
						onClick={() => attachmentInput?.click()}
					>
						<WebIcon name="attach" />
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
						class="composer-command-button"
						type="button"
						data-variant="ghost"
						data-size="small"
						aria-label="Open commands"
						aria-keyshortcuts="Control+P"
						title="Commands (Ctrl+P)"
						onClick={openPalette}
					>
						<WebIcon name="command" />
					</button>
					<button
						class="composer-send"
						type={streaming() ? "button" : "submit"}
						data-variant={streaming() ? "danger" : "primary"}
						data-size="small"
						disabled={
							!enabled() ||
							(!streaming() && (!hasPayload() || sendCoolingDown()))
						}
						aria-label={
							streaming()
								? "Abort response"
								: snapshot().submitting
									? "Sending message"
									: "Send message"
						}
						title={streaming() ? "Abort response" : "Send message"}
						onClick={(event) => {
							if (!streaming()) return;
							event.preventDefault();
							void controller.abort();
						}}
					>
						{snapshot().submitting && !streaming() ? (
							<span class="composer-submit-spinner" />
						) : (
							<WebIcon name={streaming() ? "stop" : "send"} />
						)}
					</button>
				</div>
			</form>
		</section>
	);
}
