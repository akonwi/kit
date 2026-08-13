/** @jsxImportSource solid-js */
import { For, type JSX, Show } from "solid-js";
import { MIDDLE_DOT } from "../shell/glyphs";

export function OverlayHintBar(props: {
	class: string;
	hints: readonly string[];
}): JSX.Element {
	return (
		<footer class={props.class}>
			<For each={props.hints}>
				{(hint, index) => (
					<>
						<Show when={index() > 0}>
							<span class="overlay-hint-separator" aria-hidden="true">
								{MIDDLE_DOT}
							</span>
						</Show>
						<span>{hint}</span>
					</>
				)}
			</For>
		</footer>
	);
}
