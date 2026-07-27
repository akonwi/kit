import { createEffect, createMemo, createSignal, For } from "solid-js";
import { useKeymapLayer } from "../keymap/useKeymapLayer";
import { ChromeContributionText } from "./ChromeContributionLine";
import type { ChromeContribution } from "./chrome-contributions";
import { CHEVRON_RIGHT } from "./glyphs";
import { scrollbarStyle, theme } from "./theme";

export type ChromeOverflowPlacement = "header" | "footer";

export type ChromeOverflowPickerProps = {
	title: string;
	placement: ChromeOverflowPlacement;
	contributions: readonly ChromeContribution[];
	width: number;
	onClose: () => void;
	onError: (error: unknown) => void;
};

export function ChromeOverflowPicker(props: ChromeOverflowPickerProps) {
	let scrollRef:
		| { scrollChildIntoView?: (childId: string) => void }
		| undefined;
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const clampedIndex = () =>
		Math.max(0, Math.min(selectedIndex(), props.contributions.length - 1));
	const selected = () => props.contributions[clampedIndex()] ?? null;

	function move(delta: number) {
		if (props.contributions.length === 0) return;
		setSelectedIndex((current) =>
			Math.max(0, Math.min(current + delta, props.contributions.length - 1)),
		);
	}

	function activate(contribution: ChromeContribution | null) {
		if (!contribution?.onClick) return;
		props.onClose();
		void Promise.resolve(contribution.onClick()).catch(props.onError);
	}

	useKeymapLayer(() => ({
		scope: "picker",
		when: () => props.contributions.length > 0,
		commands: {
			"chrome-overflow.move-up": () => move(-1),
			"chrome-overflow.move-down": () => move(1),
			"chrome-overflow.activate": () => activate(selected()),
			"chrome-overflow.close": props.onClose,
		},
	}));

	createEffect(() => {
		const contribution = selected();
		if (!contribution) return;
		queueMicrotask(() =>
			scrollRef?.scrollChildIntoView?.(`chrome-overflow:${contribution.id}`),
		);
	});

	const rows = createMemo(() =>
		props.contributions.map((contribution) => ({
			contribution,
			displayContribution: { ...contribution, onClick: undefined },
		})),
	);

	return (
		<box
			position="absolute"
			right={1}
			top={props.placement === "header" ? 2 : undefined}
			bottom={props.placement === "footer" ? 2 : undefined}
			width={props.width}
			maxHeight={12}
			zIndex={50}
			backgroundColor={theme.pickerBg}
			paddingY={1}
			flexDirection="column"
			onMouseDown={(event) => {
				event.preventDefault();
				event.stopPropagation();
			}}
		>
			<box flexShrink={0} paddingX={1} paddingBottom={1}>
				<text fg={theme.textMuted}>{props.title}</text>
			</box>
			<scrollbox
				ref={(value) => {
					scrollRef = value as typeof scrollRef;
				}}
				flexGrow={1}
				maxHeight={9}
				scrollY
				style={scrollbarStyle()}
			>
				<For each={rows()}>
					{(row, index) => {
						const focused = () => index() === clampedIndex();
						return (
							<box
								id={`chrome-overflow:${row.contribution.id}`}
								width="100%"
								height={1}
								paddingX={1}
								flexDirection="row"
								justifyContent="space-between"
								backgroundColor={
									focused() ? theme.pickerFocusedBg : theme.pickerBg
								}
								onMouseUp={(event) => {
									event.preventDefault();
									event.stopPropagation();
									setSelectedIndex(index());
									activate(row.contribution);
								}}
							>
								<ChromeContributionText
									contribution={row.displayContribution}
									fg={theme.textSecondary}
									focused={focused()}
								/>
								<text
									fg={focused() ? theme.pickerFocusedText : theme.textMuted}
									bg={focused() ? theme.pickerFocusedBg : theme.pickerBg}
								>
									{row.contribution.onClick ? CHEVRON_RIGHT : ""}
								</text>
							</box>
						);
					}}
				</For>
			</scrollbox>
		</box>
	);
}
