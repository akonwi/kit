/** @jsxImportSource solid-js */
import { createMemo, type JSX, Show } from "solid-js";
import { BUILT_IN_CHROME_CONTRIBUTION_IDS } from "../shell/chrome-contributions";
import { AgentConfigurationControls } from "./AgentConfigurationControls";
import { RemoteChromeLine } from "./chrome-contributions";
import { useWebClient } from "./WebClientContext";

export function HeaderBar(): JSX.Element {
	const { snapshot, controller } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const session = createMemo(() => {
		const name = protocol().serverState.sessionName;
		return typeof name === "string" && name.trim() ? name : "Unnamed session";
	});
	const hidden = createMemo(
		() => new Set(protocol().chrome.header.hiddenBuiltinIds),
	);
	const disabled = createMemo(() => protocol().phase !== "live");
	const activate = (area: "header" | "footer", contributionId: string) => {
		void controller.activateChromeContribution(area, contributionId);
	};

	return (
		<header class="header-bar">
			<div class="header-chrome-side header-chrome-left">
				<Show
					when={!hidden().has(BUILT_IN_CHROME_CONTRIBUTION_IDS.headerTitle)}
				>
					<h1 class="header-title" title={session()}>
						{session()}
					</h1>
				</Show>
				<RemoteChromeLine
					area="header"
					contributions={protocol().chrome.header.contributions}
					side="left"
					leadingSeparator={
						!hidden().has(BUILT_IN_CHROME_CONTRIBUTION_IDS.headerTitle)
					}
					disabled={disabled()}
					onActivate={activate}
				/>
			</div>
			<div class="header-chrome-side header-chrome-right">
				<Show
					when={!hidden().has(BUILT_IN_CHROME_CONTRIBUTION_IDS.headerModel)}
				>
					<AgentConfigurationControls />
				</Show>
				<RemoteChromeLine
					area="header"
					contributions={protocol().chrome.header.contributions}
					side="right"
					leadingSeparator={
						!hidden().has(BUILT_IN_CHROME_CONTRIBUTION_IDS.headerModel)
					}
					disabled={disabled()}
					onActivate={activate}
				/>
			</div>
		</header>
	);
}
