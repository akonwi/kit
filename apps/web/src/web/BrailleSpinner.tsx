/** @jsxImportSource solid-js */
import { For, type JSX } from "solid-js";
import { SPINNER_FRAMES } from "../shell/glyphs";

export function BrailleSpinner(props: { class?: string }): JSX.Element {
	return (
		<span
			class={`braille-spinner${props.class ? ` ${props.class}` : ""}`}
			aria-hidden="true"
		>
			<For each={SPINNER_FRAMES}>
				{(frame, index) => (
					<span
						class="braille-spinner-frame"
						style={{ "--spinner-frame-index": index() }}
					>
						{frame}
					</span>
				)}
			</For>
		</span>
	);
}
