import type { BoxProps, TextProps } from "@opentui/solid";
import { type JSX, splitProps } from "solid-js";
import type { OverlaySurfaceProps } from "../app/overlay-ui";
import { theme } from "./theme";

// ── Root ────────────────────────────────────────────────────────────

type RootProps = {
	children: JSX.Element;
	width?: number | `${number}%`;
	maxWidth?: number;
	minWidth?: number;
	height?: number | `${number}%`;
	padding?: number;
	paddingX?: number;
	paddingY?: number;
	paddingTop?: number;
	paddingBottom?: number;
	paddingLeft?: number;
	paddingRight?: number;
	surfaceProps?: OverlaySurfaceProps;
};

function Root(props: RootProps) {
	return (
		<box
			{...props.surfaceProps}
			position="absolute"
			left={0}
			top={0}
			width="100%"
			height="100%"
			justifyContent="center"
			alignItems="center"
			backgroundColor={theme.modalBackdrop}
		>
			<box
				width={props.width ?? "70%"}
				maxWidth={props.maxWidth ?? 96}
				minWidth={props.minWidth ?? 48}
				height={props.height}
				border
				borderColor={theme.borderDefault}
				backgroundColor={theme.bgSurface}
				padding={props.padding ?? 1}
				paddingX={props.paddingX}
				paddingY={props.paddingY}
				paddingTop={props.paddingTop}
				paddingBottom={props.paddingBottom}
				paddingLeft={props.paddingLeft}
				paddingRight={props.paddingRight}
				flexDirection="column"
				gap={1}
				overflow="hidden"
			>
				{props.children}
			</box>
		</box>
	);
}

// ── Header ──────────────────────────────────────────────────────────

type HeaderProps = {
	children: JSX.Element;
};

function Header(props: HeaderProps) {
	return (
		<box flexShrink={0} flexDirection="row" justifyContent="space-between">
			{props.children}
		</box>
	);
}

function Title(props: TextProps) {
	const [picked, ...rest] = splitProps(props, ["fg"]);
	const fg = picked.fg ?? theme.textPrimary;
	return (
		<text fg={fg} {...rest}>
			{props.children}
		</text>
	);
}

// ── Meta ────────────────────────────────────────────────────────────

type MetaProps = {
	children: JSX.Element;
};

function Meta(props: MetaProps) {
	return <text fg={theme.textMuted}>{props.children}</text>;
}

// ── Body ────────────────────────────────────────────────────────────

type BodyProps = {
	children: JSX.Element;
};

function Body(props: BodyProps) {
	return (
		<box flexGrow={1} flexDirection="column">
			{props.children}
		</box>
	);
}

// ── Footer ──────────────────────────────────────────────────────────

function Footer(props: BoxProps) {
	return (
		<box {...props} flexShrink={0}>
			{props.children}
		</box>
	);
}

// ── Export ───────────────────────────────────────────────────────────

export const Dialog = {
	Root,
	Header,
	Title,
	Meta,
	Body,
	Footer,
};
