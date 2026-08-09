/** @jsxImportSource solid-js */
import type { JSX } from "solid-js";
import { Composer } from "./Composer";
import { InteractionDialog } from "./InteractionDialog";
import { SessionHeader } from "./SessionHeader";
import { Transcript } from "./Transcript";

export function App(): JSX.Element {
	return (
		<>
			<a href="#transcript" data-visually-hidden="focusable">
				Skip to transcript
			</a>
			<m-vstack class="app-shell" gap="none">
				<SessionHeader />
				<Transcript />
				<Composer />
			</m-vstack>
			<InteractionDialog />
		</>
	);
}
