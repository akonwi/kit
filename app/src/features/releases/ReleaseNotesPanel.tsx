import { createSignal, onCleanup, Show } from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { TRIANGLE_UP } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { scrollbarStyle, syntaxStyle, theme } from "../../shell/theme";
import { WorkspacePanelLayout } from "../../shell/WorkspacePanelLayout";
import type {
	ReleasesState,
	ReleasesWorkspaceController,
} from "./workspace-controller";

export const RELEASE_NOTES_MIN_COLS = 40;

type ScrollRef = {
	scrollBy: (options: { x: number; y: number }) => void;
};

export type ReleaseNotesPanelProps = {
	controller: ReleasesWorkspaceController;
	active?: boolean;
	onClose: () => void;
	onFocusRequest?: () => void;
	onOpenRelease: (url: string) => Promise<void>;
	onOpenError: (error: unknown) => void;
};

export function ReleaseNotesPanel(props: ReleaseNotesPanelProps) {
	const [state, setState] = createSignal<ReleasesState>(
		props.controller.getState(),
	);
	onCleanup(props.controller.subscribe(setState));
	let scrollRef: ScrollRef | undefined;
	const active = () => props.active !== false;

	async function openLatest(): Promise<void> {
		const latest = state().latest;
		if (!latest) return;
		try {
			await props.onOpenRelease(latest.url);
		} catch (error) {
			props.onOpenError(error);
		}
	}

	useKeymapLayer(() => ({
		scope: "panel",
		when: active,
		diagnosticsWhen: active,
		commands: {
			"release-notes.close": props.onClose,
			"release-notes.scroll-up": () => {
				scrollRef?.scrollBy({ x: 0, y: -1 });
			},
			"release-notes.scroll-down": () => {
				scrollRef?.scrollBy({ x: 0, y: 1 });
			},
			...(state().latest ? { "release-notes.open-latest": openLatest } : {}),
		},
	}));

	return (
		<box
			width="100%"
			height="100%"
			onMouseDown={(event) => {
				if (event.button === 0) props.onFocusRequest?.();
			}}
		>
			<WorkspacePanelLayout
				header={
					<box
						flexShrink={0}
						paddingX={1}
						border={["bottom"]}
						borderColor={theme.borderDefault}
						flexDirection="row"
						justifyContent="space-between"
					>
						<text fg={theme.textPrimary}>Release notes</text>
						<text fg={theme.textMuted}>v{state().currentVersion}</text>
					</box>
				}
				footer={<KeymapHintBar group="release-notes" borderless />}
			>
				<Show when={state().latest}>
					{(latest) => (
						<box flexShrink={0} paddingX={1} height={1} overflow="hidden">
							<text fg={theme.warningText} wrapMode="none">
								{TRIANGLE_UP} v{latest().version} available
							</text>
						</box>
					)}
				</Show>
				<scrollbox
					ref={(value) => {
						scrollRef = value as ScrollRef;
					}}
					flexGrow={1}
					paddingX={1}
					paddingY={1}
					scrollY
					style={scrollbarStyle()}
				>
					<markdown
						content={state().currentNotes}
						syntaxStyle={syntaxStyle()}
						conceal
					/>
				</scrollbox>
			</WorkspacePanelLayout>
		</box>
	);
}
