import type { BoxProps, TextProps } from "@opentui/solid";
import { type JSX, splitProps } from "solid-js";
import { DIAMOND } from "./glyphs";
import { theme } from "./theme";

type RootProps = {
	children: JSX.Element;
	maxHeight: number;
};

function Root(props: RootProps) {
	return (
		<box
			width="100%"
			maxHeight={props.maxHeight}
			flexShrink={0}
			flexDirection="column"
			gap={1}
			paddingX={1}
			border={["top"]}
			borderColor={theme.borderAccent}
			backgroundColor={theme.bgSurface}
			overflow="hidden"
		>
			{props.children}
		</box>
	);
}

type HeaderProps = {
	children: JSX.Element;
	meta?: JSX.Element;
};

function Header(props: HeaderProps) {
	return (
		<box
			flexShrink={0}
			flexDirection="row"
			justifyContent="space-between"
			gap={1}
			overflow="hidden"
		>
			<box flexDirection="row" gap={1} flexShrink={1} overflow="hidden">
				<text fg={theme.borderAccent}>{DIAMOND}</text>
				{props.children}
			</box>
			{props.meta}
		</box>
	);
}

function Title(props: TextProps) {
	const [picked, rest] = splitProps(props, ["fg", "flexShrink", "overflow"]);
	return (
		<text
			fg={picked.fg ?? theme.textPrimary}
			flexShrink={picked.flexShrink ?? 1}
			overflow={picked.overflow ?? "hidden"}
			{...rest}
		>
			{props.children}
		</text>
	);
}

type MetaProps = { children: JSX.Element };

function Meta(props: MetaProps) {
	return (
		<text fg={theme.textMuted} flexShrink={0}>
			{props.children}
		</text>
	);
}

type BodyProps = BoxProps & { children: JSX.Element };

function Body(props: BodyProps) {
	const [local, rest] = splitProps(props, ["children"]);
	return (
		<box
			{...rest}
			flexGrow={1}
			flexShrink={1}
			flexDirection="column"
			overflow="hidden"
		>
			{local.children}
		</box>
	);
}

type FooterProps = BoxProps;

function Footer(props: FooterProps) {
	const [local, rest] = splitProps(props, [
		"children",
		"border",
		"borderColor",
	]);
	return (
		<box
			{...rest}
			flexShrink={0}
			border={["top"]}
			borderColor={theme.borderDefault}
		>
			{local.children}
		</box>
	);
}

export const InteractionDock = { Root, Header, Title, Meta, Body, Footer };
