/** @jsxImportSource solid-js */
import { createMemo, type JSX } from "solid-js";
import { AgentConfigurationControls } from "./AgentConfigurationControls";
import { useWebClient } from "./WebClientContext";

export function HeaderBar(): JSX.Element {
	const { snapshot } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const session = createMemo(() => {
		const name = protocol().serverState.sessionName;
		return typeof name === "string" && name.trim() ? name : "Unnamed session";
	});
	return (
		<header class="header-bar">
			<h1 class="header-title" title={session()}>
				{session()}
			</h1>
			<AgentConfigurationControls />
		</header>
	);
}
