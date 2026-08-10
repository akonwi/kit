/** @jsxImportSource solid-js */
import type { JSX } from "solid-js";
import { AgentConfigurationProvider } from "./AgentConfigurationControls";
import { AppShell } from "./AppShell";
import { BrowserThemeProvider } from "./BrowserThemeProvider";
import { CommandPalette } from "./CommandPalette";
import { InteractionDialog } from "./InteractionDialog";
import { OpenUrlRequests } from "./OpenUrlRequests";
import { ScratchpadProvider } from "./ScratchpadProvider";

export function App(): JSX.Element {
	return (
		<BrowserThemeProvider>
			<ScratchpadProvider>
				<AgentConfigurationProvider>
					<a href="#transcript" data-visually-hidden="focusable">
						Skip to transcript
					</a>
					<AppShell />
					<CommandPalette />
					<OpenUrlRequests />
					<InteractionDialog />
				</AgentConfigurationProvider>
			</ScratchpadProvider>
		</BrowserThemeProvider>
	);
}
