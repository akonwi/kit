/** @jsxImportSource solid-js */
import {
	createMemo,
	createSignal,
	For,
	type JSX,
	onCleanup,
	onMount,
	Show,
} from "solid-js";
import { BUILT_IN_CHROME_CONTRIBUTION_IDS } from "../shell/chrome-contributions";
import { CHEVRON_RIGHT, ELLIPSIS } from "../shell/glyphs";
import { useAgentConfiguration } from "./AgentConfigurationControls";
import { RemoteChromeContributionView } from "./chrome-contributions";
import { DialogFrame } from "./DialogFrame";
import { useScratchpad } from "./ScratchpadProvider";
import { useWebClient } from "./WebClientContext";

export function MobileSessionMenu(): JSX.Element {
	const { snapshot, controller } = useWebClient();
	const scratchpad = useScratchpad();
	const {
		disabled: configurationDisabled,
		modelLabel,
		thinkingLevel,
		openModelPicker,
		openThinkingPicker,
	} = useAgentConfiguration();
	const [open, setOpen] = createSignal(false);
	const protocol = createMemo(() => snapshot().protocol);
	const disabled = createMemo(() => protocol().phase !== "live");
	const hiddenHeader = createMemo(
		() => new Set(protocol().chrome.header.hiddenBuiltinIds),
	);
	const hiddenFooter = createMemo(
		() => new Set(protocol().chrome.footer.hiddenBuiltinIds),
	);
	const cwd = createMemo(() => {
		const value = protocol().serverState.cwd;
		return typeof value === "string" ? value : "";
	});
	const hasRemoteLocation = createMemo(() =>
		protocol().chrome.footer.contributions.some(
			(contribution) =>
				contribution.id === BUILT_IN_CHROME_CONTRIBUTION_IDS.footerLocation,
		),
	);
	const locationVisible = createMemo(
		() =>
			cwd().length > 0 &&
			!hasRemoteLocation() &&
			!hiddenFooter().has(BUILT_IN_CHROME_CONTRIBUTION_IDS.footerLocation),
	);

	const closeThen = (action: () => void) => {
		setOpen(false);
		requestAnimationFrame(action);
	};
	onMount(() => {
		const mobileViewport = window.matchMedia("(max-width: 36rem)");
		const closeOutsideMobile = () => {
			if (!mobileViewport.matches) setOpen(false);
		};
		mobileViewport.addEventListener("change", closeOutsideMobile);
		onCleanup(() =>
			mobileViewport.removeEventListener("change", closeOutsideMobile),
		);
	});

	const activate = (area: "header" | "footer", contributionId: string) => {
		setOpen(false);
		queueMicrotask(() => {
			void controller.activateChromeContribution(area, contributionId);
		});
	};

	return (
		<>
			<button
				class="mobile-session-menu-trigger"
				type="button"
				data-variant="ghost"
				aria-label="Open session controls"
				aria-haspopup="dialog"
				aria-expanded={open()}
				onClick={() => setOpen(true)}
			>
				<span aria-hidden="true">{ELLIPSIS}</span>
			</button>
			<DialogFrame
				open={open()}
				id="mobile-session-menu"
				class="mobile-session-menu-dialog"
				labelledBy="mobile-session-menu-title"
				onCancel={() => setOpen(false)}
				onAfterOpen={(dialog) =>
					dialog.querySelector<HTMLElement>("button:not(:disabled)")?.focus()
				}
			>
				<header>
					<h2 id="mobile-session-menu-title">Session controls</h2>
				</header>
				<div class="mobile-session-menu-body">
					<Show
						when={
							!hiddenHeader().has(BUILT_IN_CHROME_CONTRIBUTION_IDS.headerModel)
						}
					>
						<button
							class="mobile-session-menu-row"
							type="button"
							disabled={configurationDisabled()}
							onClick={() => closeThen(() => void openModelPicker())}
						>
							<span>Model</span>
							<span class="mobile-session-menu-value">
								{modelLabel() || "Unavailable"}
							</span>
						</button>
						<button
							class="mobile-session-menu-row"
							type="button"
							disabled={configurationDisabled()}
							onClick={() => closeThen(() => void openThinkingPicker())}
						>
							<span>Thinking</span>
							<span class="mobile-session-menu-value">
								{thinkingLevel() || "Unavailable"}
							</span>
						</button>
					</Show>
					<button
						class="mobile-session-menu-row"
						type="button"
						disabled={scratchpad.disabled()}
						onClick={() => closeThen(scratchpad.toggle)}
					>
						<span>Scratchpad</span>
						<span class="mobile-session-menu-value">
							{scratchpad.open() ? "Open" : CHEVRON_RIGHT}
						</span>
					</button>
					<For each={protocol().chrome.header.contributions}>
						{(contribution) => (
							<div class="mobile-session-menu-row mobile-session-menu-contribution">
								<RemoteChromeContributionView
									area="header"
									contribution={contribution}
									disabled={disabled()}
									onActivate={activate}
								/>
							</div>
						)}
					</For>
					<Show when={locationVisible()}>
						<div class="mobile-session-menu-row mobile-session-menu-location">
							<span>Location</span>
							<span class="mobile-session-menu-value" title={cwd()}>
								{cwd()}
							</span>
						</div>
					</Show>
					<For each={protocol().chrome.footer.contributions}>
						{(contribution) => (
							<div class="mobile-session-menu-row mobile-session-menu-contribution">
								<RemoteChromeContributionView
									area="footer"
									contribution={contribution}
									disabled={disabled()}
									onActivate={activate}
								/>
							</div>
						)}
					</For>
				</div>
			</DialogFrame>
		</>
	);
}
