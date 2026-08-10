/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	createSignal,
	type JSX,
	Show,
} from "solid-js";
import { BUILT_IN_CHROME_CONTRIBUTION_IDS } from "../shell/chrome-contributions";
import { AgentConfigurationControls } from "./AgentConfigurationControls";
import { RemoteChromeLine } from "./chrome-contributions";
import { SessionNameDialog } from "./SessionNameDialog";
import { useWebClient } from "./WebClientContext";

export function HeaderBar(): JSX.Element {
	const { snapshot, controller } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const sessionName = createMemo(() => {
		const name = protocol().serverState.sessionName;
		return typeof name === "string" ? name : "";
	});
	const sessionLabel = createMemo(() =>
		sessionName().trim() ? sessionName() : "Unnamed session",
	);
	const sessionId = createMemo(() => {
		const id = protocol().serverState.sessionId;
		return typeof id === "string" ? id : null;
	});
	const [nameDialogOpen, setNameDialogOpen] = createSignal(false);
	let observedStreamId: string | null = null;
	let observedSessionId: string | null = null;
	const hidden = createMemo(
		() => new Set(protocol().chrome.header.hiddenBuiltinIds),
	);
	const disabled = createMemo(() => protocol().phase !== "live");
	const namingDisabled = createMemo(
		() =>
			disabled() ||
			protocol().serverState.isStreaming === true ||
			snapshot().submitting,
	);
	const activate = (area: "header" | "footer", contributionId: string) => {
		void controller.activateChromeContribution(area, contributionId);
	};

	createEffect(() => {
		const state = protocol();
		const nextSessionId = sessionId();
		if (
			nameDialogOpen() &&
			(namingDisabled() ||
				(observedStreamId !== null && state.streamId !== observedStreamId) ||
				(observedSessionId !== null && nextSessionId !== observedSessionId))
		) {
			setNameDialogOpen(false);
		}
		observedStreamId = state.streamId;
		observedSessionId = nextSessionId;
	});

	return (
		<>
			<header class="header-bar">
				<div class="header-chrome-side header-chrome-left">
					<Show
						when={!hidden().has(BUILT_IN_CHROME_CONTRIBUTION_IDS.headerTitle)}
					>
						<h1
							class="header-title"
							title={sessionLabel()}
							aria-label={sessionLabel()}
						>
							<button
								class="header-title-action"
								type="button"
								data-variant="ghost"
								disabled={namingDisabled()}
								aria-label={`Rename session, current ${sessionLabel()}`}
								onClick={() => setNameDialogOpen(true)}
							>
								{sessionLabel()}
							</button>
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
			<SessionNameDialog
				open={nameDialogOpen()}
				currentName={sessionName()}
				onCancel={() => setNameDialogOpen(false)}
				onSubmit={(name) => controller.renameSession(name)}
			/>
		</>
	);
}
