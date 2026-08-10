/** @jsxImportSource solid-js */
import { createEffect, createMemo, type JSX, Show } from "solid-js";
import { TIMES } from "../shell/glyphs";
import { useScratchpad } from "./ScratchpadProvider";

export function ScratchpadPanel(): JSX.Element {
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
		if (scratchpad.loading()) return;
		queueMicrotask(() => input?.focus());
	});

	return (
		<section
			class="scratchpad-panel"
			aria-label="Scratchpad"
			onKeyDown={(event) => {
				if (event.key !== "Escape") return;
				event.preventDefault();
				event.stopPropagation();
				scratchpad.close();
			}}
		>
			<header class="scratchpad-panel-header">
				<h2>Scratchpad</h2>
				<span
					class="scratchpad-panel-status"
					classList={{ "is-error": scratchpad.error().length > 0 }}
					role="status"
					aria-live="polite"
				>
					{status()}
				</span>
				<button
					type="button"
					data-variant="ghost"
					data-size="small"
					aria-label="Close scratchpad"
					title="Close (Esc)"
					onClick={scratchpad.close}
				>
					{TIMES}
				</button>
			</header>
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
				onKeyDown={(event) => {
					if (event.key !== "Escape") return;
					event.preventDefault();
					event.stopPropagation();
					scratchpad.close();
				}}
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
