/** @jsxImportSource solid-js */
import { createEffect, createMemo, type JSX, Show } from "solid-js";
import { useScratchpad } from "./ScratchpadProvider";

export function ScratchpadPanel(props: { active: boolean }): JSX.Element {
	const scratchpad = useScratchpad();
	let input: HTMLTextAreaElement | undefined;
	const status = createMemo(() => {
		if (scratchpad.loading()) return "Loading…";
		if (scratchpad.saving()) return "Saving…";
		if (scratchpad.error()) return scratchpad.error();
		if (scratchpad.dirty()) return "Unsaved";
		return "";
	});

	createEffect(() => {
		if (scratchpad.loading() || !props.active) return;
		queueMicrotask(() => input?.focus());
	});

	return (
		<section class="scratchpad-panel" aria-label="Scratchpad">
			<Show when={status()}>
				<header class="workspace-panel-context">
					<span
						class="scratchpad-panel-status"
						classList={{ "is-error": scratchpad.error().length > 0 }}
						role="status"
						aria-live="polite"
					>
						{status()}
					</span>
				</header>
			</Show>
			<label for="scratchpad-editor" data-visually-hidden>
				Scratchpad notes
			</label>
			<textarea
				ref={input}
				id="scratchpad-editor"
				class="scratchpad-editor"
				value={scratchpad.draft()}
				placeholder={scratchpad.loading() ? "Loading…" : "Type notes..."}
				disabled={scratchpad.loading()}
				onInput={(event) => scratchpad.setDraft(event.currentTarget.value)}
			/>
			<Show when={scratchpad.error()}>
				<footer class="scratchpad-panel-footer">
					<button
						type="button"
						data-variant="ghost"
						data-size="small"
						disabled={scratchpad.loading() || scratchpad.saving()}
						onClick={scratchpad.reload}
					>
						Reload from disk
					</button>
				</footer>
			</Show>
		</section>
	);
}
