/** @jsxImportSource solid-js */
import { createMemo, type JSX, Show } from "solid-js";
import { BUILT_IN_CHROME_CONTRIBUTION_IDS } from "../shell/chrome-contributions";
import { RemoteChromeLine } from "./chrome-contributions";
import { useWebClient } from "./WebClientContext";

function StatusBarFrame(props: { class: string }): JSX.Element {
	const { snapshot, controller } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const connectionStatus = createMemo(() => {
		switch (protocol().phase) {
			case "connecting":
				return "Connecting…";
			case "synchronizing":
				return "Synchronizing…";
			case "disconnected":
				return "Disconnected";
			default:
				return "";
		}
	});
	const guidance = createMemo(() => {
		const connection = connectionStatus();
		if (connection) {
			return snapshot().status.message
				? `${connection} · ${snapshot().status.message}`
				: connection;
		}
		if (snapshot().status.message) return snapshot().status.message;
		if (protocol().queuedMessageCount > 0) {
			return `queued messages: ${protocol().queuedMessageCount}`;
		}
		return "";
	});
	const cwd = createMemo(() => {
		const value = protocol().serverState.cwd;
		return typeof value === "string" ? value : "";
	});
	const hidden = createMemo(
		() => new Set(protocol().chrome.footer.hiddenBuiltinIds),
	);
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
			!hidden().has(BUILT_IN_CHROME_CONTRIBUTION_IDS.footerLocation),
	);
	const disabled = createMemo(() => protocol().phase !== "live");
	const activate = (area: "header" | "footer", contributionId: string) => {
		void controller.activateChromeContribution(area, contributionId);
	};

	return (
		<footer
			class={`bottom-status-bar ${props.class}`}
			data-phase={protocol().phase}
		>
			<span data-visually-hidden role="status" aria-live="polite">
				{protocol().phase === "live" ? "Connected" : ""}
			</span>
			<div class="footer-chrome-side footer-chrome-left">
				<span
					class="bottom-guidance"
					classList={{ "is-error": snapshot().status.isError }}
					role="status"
					aria-live="polite"
				>
					{guidance()}
				</span>
				<Show when={protocol().phase === "disconnected"}>
					<button
						class="bottom-reconnect"
						type="button"
						data-variant="ghost"
						data-size="small"
						onClick={() => controller.reconnect()}
					>
						Reconnect
					</button>
				</Show>
				<RemoteChromeLine
					area="footer"
					contributions={protocol().chrome.footer.contributions}
					side="left"
					leadingSeparator={guidance().length > 0}
					disabled={disabled()}
					onActivate={activate}
				/>
			</div>
			<div class="footer-chrome-side footer-chrome-right">
				<Show when={locationVisible()}>
					<span class="bottom-location" title={cwd()}>
						{cwd()}
					</span>
				</Show>
				<RemoteChromeLine
					area="footer"
					contributions={protocol().chrome.footer.contributions}
					side="right"
					leadingSeparator={locationVisible()}
					disabled={disabled()}
					onActivate={activate}
				/>
			</div>
		</footer>
	);
}

export function DesktopStatusBar(): JSX.Element {
	return <StatusBarFrame class="desktop-status-bar" />;
}

export function MobileStatusBar(): JSX.Element {
	const { snapshot } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const visible = createMemo(
		() =>
			protocol().phase !== "live" ||
			Boolean(snapshot().status.message) ||
			protocol().queuedMessageCount > 0,
	);
	return (
		<Show when={visible()}>
			<StatusBarFrame class="mobile-status-bar" />
		</Show>
	);
}
