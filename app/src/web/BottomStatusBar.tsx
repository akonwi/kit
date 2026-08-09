/** @jsxImportSource solid-js */
import { createMemo, type JSX, Show } from "solid-js";
import { useWebClient } from "./WebClientContext";

export function BottomStatusBar(): JSX.Element {
	const { snapshot } = useWebClient();
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

	return (
		<footer class="bottom-status-bar" data-phase={protocol().phase}>
			<span data-visually-hidden role="status" aria-live="polite">
				{protocol().phase === "live" ? "Connected" : ""}
			</span>
			<span
				class="bottom-guidance"
				classList={{ "is-error": snapshot().status.isError }}
				role="status"
				aria-live="polite"
			>
				{guidance()}
			</span>
			<Show when={cwd()}>
				{(value) => (
					<span class="bottom-location" title={value()}>
						{value()}
					</span>
				)}
			</Show>
		</footer>
	);
}
