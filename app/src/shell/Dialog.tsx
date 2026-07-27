import type { Renderable } from "@opentui/core";
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
	gap?: number;
	padding?: number;
	paddingX?: number;
	paddingY?: number;
	paddingTop?: number;
	paddingBottom?: number;
	paddingLeft?: number;
	paddingRight?: number;
	surfaceProps?: OverlaySurfaceProps;
	rootRef?: (value: Renderable) => void;
	rootFocusable?: boolean;
	rootFocused?: boolean;
};

function Root(props: RootProps) {
	return (
		<box
			{...props.surfaceProps}
			ref={(value) => props.rootRef?.(value as Renderable)}
			focusable={props.rootFocusable}
			focused={props.rootFocused}
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
				gap={props.gap ?? 1}
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
	strip?: boolean;
};

function Header(props: HeaderProps) {
	return (
		<box
			flexShrink={0}
			flexDirection="row"
			justifyContent="space-between"
			gap={1}
			paddingX={props.strip ? 1 : undefined}
			height={props.strip ? 2 : undefined}
			border={props.strip ? ["bottom"] : false}
			borderColor={props.strip ? theme.borderDefault : undefined}
			overflow="hidden"
		>
			{props.children}
		</box>
	);
}

function Title(props: TextProps) {
	const [picked, rest] = splitProps(props, ["fg", "flexShrink", "overflow"]);
	const fg = picked.fg ?? theme.textPrimary;
	return (
		<text
			fg={fg}
			flexShrink={picked.flexShrink ?? 1}
			overflow={picked.overflow ?? "hidden"}
			{...rest}
		>
			{props.children}
		</text>
	);
}

// ── Meta ────────────────────────────────────────────────────────────

type MetaProps = {
	children: JSX.Element;
};

function Meta(props: MetaProps) {
	return (
		<text fg={theme.textMuted} flexShrink={0}>
			{props.children}
		</text>
	);
}

// ── Body ────────────────────────────────────────────────────────────

type BodyProps = BoxProps & {
	children: JSX.Element;
};

function Body(props: BodyProps) {
	const [local, rest] = splitProps(props, ["children"]);
	return (
		<box {...rest} flexGrow={1} flexDirection="column">
			{local.children}
		</box>
	);
}

// ── Footer ──────────────────────────────────────────────────────────

type FooterProps = BoxProps & {
	strip?: boolean;
};

function Footer(props: FooterProps) {
	const [local, rest] = splitProps(props, [
		"strip",
		"children",
		"paddingX",
		"border",
		"borderColor",
	]);
	return (
		<box
			{...rest}
			flexShrink={0}
			paddingX={local.strip ? 1 : local.paddingX}
			border={local.strip ? ["top"] : local.border}
			borderColor={local.strip ? theme.borderDefault : local.borderColor}
		>
			{local.children}
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
