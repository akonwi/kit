import { useRenderer } from "@opentui/solid";
import { createMemo, createSignal, onCleanup } from "solid-js";
import type { ToolCall } from "../../runtime/agent";
import { theme } from "../theme";
import { subagentToolAgentName, toolDisplayName } from "./turns";

export function ToolCallName(props: {
	tc: ToolCall;
	args?: Record<string, unknown>;
	color: string;
	attributes?: number;
	onOpenSubagent?: (agentName: string) => boolean;
}) {
	const renderer = useRenderer();
	const [hovered, setHovered] = createSignal(false);
	const agentName = createMemo(() =>
		subagentToolAgentName(props.tc, props.args),
	);
	const clickable = () => agentName() !== null && !!props.onOpenSubagent;

	function stopHover(): void {
		if (!hovered()) return;
		setHovered(false);
		renderer.setMousePointer("default");
	}

	onCleanup(stopHover);

	return (
		<text
			fg={hovered() && clickable() ? theme.textPrimary : props.color}
			bg={hovered() && clickable() ? theme.bgMuted : undefined}
			attributes={props.attributes}
			wrapMode="none"
			flexShrink={0}
			onMouseOver={() => {
				if (!clickable()) return;
				setHovered(true);
				renderer.setMousePointer("pointer");
			}}
			onMouseOut={stopHover}
			onMouseDown={(event) => {
				const target = agentName();
				if (!target || !props.onOpenSubagent || event.button !== 0) return;
				if (renderer.getSelection()?.getSelectedText()) return;
				if (!props.onOpenSubagent(target)) return;
				event.preventDefault();
				event.stopPropagation();
			}}
			onMouseUp={(event) => {
				if (!clickable() || event.button !== 0) return;
				event.preventDefault();
				event.stopPropagation();
			}}
		>
			{agentName() ?? toolDisplayName(props.tc)}
		</text>
	);
}
