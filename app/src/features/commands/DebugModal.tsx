import type { Renderable } from "@opentui/core";
import { createSignal, For, Show } from "solid-js";
import type { OverlaySurfaceProps } from "../../app/overlay-ui";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { Dialog } from "../../shell/Dialog";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { theme } from "../../shell/theme";

// ── Types ───────────────────────────────────────────────────────────

export type DebugEntry = { label: string; value: string };

export type DebugSection = {
	title: string;
	entries: DebugEntry[];
};

export type DebugModalProps = {
	sections: DebugSection[];
	active?: boolean;
	surfaceProps?: OverlaySurfaceProps;
	onClose: () => void;
};

// ── Components ──────────────────────────────────────────────────────

function SectionHeader(props: { title: string }) {
	return (
		<text fg={theme.textMuted}>
			<b>{props.title}</b>
		</text>
	);
}

function EntryRow(props: { label: string; value: string }) {
	return (
		<box flexDirection="row" gap={1}>
			<text fg={theme.textMuted}>{props.label}</text>
			<text fg={theme.textSecondary}>{props.value}</text>
		</box>
	);
}

function Section(props: { section: DebugSection }) {
	return (
		<Show when={props.section.entries.length > 0}>
			<box flexDirection="column">
				<SectionHeader title={props.section.title} />
				<For each={props.section.entries}>
					{(entry) => <EntryRow label={entry.label} value={entry.value} />}
				</For>
			</box>
		</Show>
	);
}

// ── Modal ───────────────────────────────────────────────────────────

export function DebugModal(props: DebugModalProps) {
	const [rootTarget, setRootTarget] = createSignal<Renderable | null>(null);

	useKeymapLayer(() => ({
		scope: "modal",
		target: rootTarget,
		targetMode: "focus-within",
		when: () => props.active !== false,
		commands: {
			"debug.close": () => props.onClose(),
		},
	}));

	return (
		<Dialog.Root
			height="70%"
			padding={0}
			surfaceProps={props.surfaceProps}
			rootRef={setRootTarget}
		>
			<Dialog.Header>
				<Dialog.Title>Debug</Dialog.Title>
			</Dialog.Header>
			<Dialog.Body>
				<scrollbox flexGrow={1} scrollY focused paddingX={1}>
					<box flexDirection="column" gap={1} width="100%">
						<For each={props.sections}>
							{(section) => <Section section={section} />}
						</For>
					</box>
				</scrollbox>
			</Dialog.Body>
			<Dialog.Footer>
				<KeymapHintBar borderless group="debug" />
			</Dialog.Footer>
		</Dialog.Root>
	);
}
