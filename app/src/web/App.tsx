/** @jsxImportSource solid-js */
import type { JSX } from "solid-js";
import { AgentConfigurationProvider } from "./AgentConfigurationControls";
import { AppShell } from "./AppShell";
import { BrowserThemeProvider } from "./BrowserThemeProvider";
import { CommandPaletteProvider } from "./CommandPalette";
import { InteractionDialog } from "./InteractionDialog";
import { OpenUrlRequests } from "./OpenUrlRequests";
import { ScratchpadProvider } from "./ScratchpadProvider";

export function App(): JSX.Element {
	return (
		<BrowserThemeProvider>
			<ScratchpadProvider>
				<AgentConfigurationProvider>
					<CommandPaletteProvider>
						<a href="#transcript" data-visually-hidden="focusable">
							Skip to transcript
						</a>
						<AppShell />
						<OpenUrlRequests />
						<InteractionDialog />
					</CommandPaletteProvider>
				</AgentConfigurationProvider>
			</ScratchpadProvider>
		</BrowserThemeProvider>
	);
}
