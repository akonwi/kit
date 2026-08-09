/** @jsxImportSource solid-js */
import { createMemo, type JSX, Show } from "solid-js";
import { isRecord } from "./client-state";
import { useWebClient } from "./WebClientContext";

export function HeaderBar(): JSX.Element {
	const { snapshot } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const session = createMemo(() => {
		const name = protocol().serverState.sessionName;
		return typeof name === "string" && name.trim() ? name : "Unnamed session";
	});
	const model = createMemo(() => {
		const value = protocol().serverState.model;
		if (!isRecord(value)) return "";
		if (typeof value.name === "string" && value.name.trim()) return value.name;
		if (typeof value.id === "string") return value.id;
		return "";
	});
	const thinking = createMemo(() => {
		const value = protocol().serverState.thinkingLevel;
		return typeof value === "string" ? value : "";
	});

	return (
		<header class="header-bar">
			<h1 class="header-title" title={session()}>
				{session()}
			</h1>
			<div class="header-meta" aria-label="Agent configuration">
				<Show when={model()}>
					{(value) => <span title={value()}>{value()}</span>}
				</Show>
				<Show when={model() && thinking()}>
					<span class="chrome-separator" aria-hidden="true">
						·
					</span>
				</Show>
				<Show when={thinking()}>{(value) => <span>{value()}</span>}</Show>
			</div>
		</header>
	);
}
