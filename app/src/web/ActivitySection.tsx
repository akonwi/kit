/** @jsxImportSource solid-js */
import { createMemo, For, type JSX, Show } from "solid-js";
import { displayValue } from "./presentation";
import { useWebClient } from "./WebClientContext";

export function ActivitySection(): JSX.Element {
	const { snapshot } = useWebClient();
	const protocol = createMemo(() => snapshot().protocol);
	const tools = createMemo(() => protocol().tools);
	const toolIds = createMemo(() => tools().map((tool) => tool.id));
	const toolsById = createMemo(
		() => new Map(tools().map((tool) => [tool.id, tool])),
	);

	return (
		<Show when={toolIds().length > 0}>
			<section class="activity" aria-label="Tool activity">
				<h2>Current activity</h2>
				<div>
					<For each={toolIds()}>
						{(id) => {
							const tool = createMemo(() => toolsById().get(id));
							return (
								<details
									class="tool-activity"
									data-status={tool()?.status}
									data-error={String(tool()?.isError === true)}
								>
									<summary>{tool()?.name ?? "Tool"}</summary>
									<pre class="tool-result">
										{displayValue(tool()?.result ?? tool()?.args)}
									</pre>
								</details>
							);
						}}
					</For>
				</div>
			</section>
		</Show>
	);
}
