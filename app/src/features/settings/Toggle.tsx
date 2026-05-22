import { theme } from "../../shell/theme";

type ToggleProps = {
	checked: boolean;
	disabled: boolean;
};

export function Toggle(props: ToggleProps) {
	const trackBackground = props.disabled
		? theme.bgMuted
		: props.checked
			? theme.toggleOn
			: theme.bgAccent;
	const knobBackground = props.disabled ? theme.textMuted : theme.textSecondary;

	return (
		<box
			width={4}
			height={1}
			backgroundColor={trackBackground}
			flexDirection="row"
			justifyContent={props.checked ? "flex-end" : "flex-start"}
			alignItems="center"
		>
			<box width={2} height={1} backgroundColor={knobBackground} />
		</box>
	);
}
