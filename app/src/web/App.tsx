/** @jsxImportSource solid-js */
import type { JSX } from "solid-js";
import { AppShell } from "./AppShell";
import { InteractionDialog } from "./InteractionDialog";

export function App(): JSX.Element {
	return (
		<>
			<a href="#transcript" data-visually-hidden="focusable">
				Skip to transcript
			</a>
			<AppShell />
			<InteractionDialog />
		</>
	);
}
