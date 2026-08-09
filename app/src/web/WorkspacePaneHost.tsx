/** @jsxImportSource solid-js */
import type { JSX } from "solid-js";

export function WorkspacePaneHost(props: {
	children: JSX.Element;
}): JSX.Element {
	return (
		<section class="workspace-pane-host" aria-label="Session workspace">
			<div class="workspace-primary">{props.children}</div>
		</section>
	);
}
