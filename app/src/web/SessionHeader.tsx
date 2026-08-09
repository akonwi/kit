/** @jsxImportSource solid-js */
import { createMemo, type JSX } from "solid-js";
import { isRecord } from "./client-state";
import { shortSessionId } from "./presentation";
import { useWebClient } from "./WebClientContext";

export function SessionHeader(): JSX.Element {
	const { snapshot } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const connectionText = createMemo(() => {
		switch (protocol().phase) {
			case "live":
				return "Connected";
			case "synchronizing":
				return "Synchronizing";
			case "connecting":
				return "Connecting";
			default:
				return "Disconnected";
		}
	});
	const workspace = createMemo<string>(() => {
		const value = protocol().serverState.cwd;
		return typeof value === "string" ? value : "";
	});
	const model = createMemo(() => {
		const value = protocol().serverState.model;
		if (!isRecord(value)) return "";
		const parts: string[] = [];
		if (typeof value.provider === "string") parts.push(value.provider);
		if (typeof value.id === "string") parts.push(value.id);
		return parts.join("/");
	});
	const session = createMemo<string>(() => {
		const name = protocol().serverState.sessionName;
		return typeof name === "string"
			? name
			: shortSessionId(protocol().serverState.sessionId);
	});

	return (
		<header class="session-header">
			<m-hstack gap="sm" align="center">
				<h1 class="wordmark">kit</h1>
				<span
					class="connection-status"
					data-phase={protocol().phase}
					role="status"
					aria-live="polite"
				>
					{connectionText()}
				</span>
			</m-hstack>
			<m-hstack class="session-meta" gap="sm" align="center">
				<span title={workspace()}>{workspace()}</span>
				<span title={model()}>{model()}</span>
				<span title={session()}>{session()}</span>
			</m-hstack>
		</header>
	);
}
