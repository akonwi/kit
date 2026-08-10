/** @jsxImportSource solid-js */
import type { JSX } from "solid-js";
import { AgentConfigurationProvider } from "./AgentConfigurationControls";
import { AppShell } from "./AppShell";
import { CommandPalette } from "./CommandPalette";
import { InteractionDialog } from "./InteractionDialog";

export function App(): JSX.Element {
	return (
		<AgentConfigurationProvider>
			<a href="#transcript" data-visually-hidden="focusable">
				Skip to transcript
			</a>
			<AppShell />
			<CommandPalette />
			<InteractionDialog />
		</AgentConfigurationProvider>
	);
}
