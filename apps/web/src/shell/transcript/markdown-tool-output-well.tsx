import { KitMarkdown } from "../KitMarkdown";
import { theme } from "../theme";
import { ToolOutputWell } from "./tool-output-well";

export function MarkdownToolOutputWell(props: {
	content: string;
	stickyBottom?: boolean;
	streaming?: boolean;
}) {
	const logicalLines = () => props.content.replace(/\r\n/g, "\n").split("\n");
	return (
		<ToolOutputWell
			measureContent
			metadata={() => {
				const count = logicalLines().length;
				return `${count} ${count === 1 ? "line" : "lines"}`;
			}}
			stickyBottom={props.stickyBottom}
		>
			<KitMarkdown
				content={props.content}
				fg={theme.textPrimary}
				streaming={props.streaming}
				flexShrink={0}
			/>
		</ToolOutputWell>
	);
}
