import { createEffect, createSignal, For, onCleanup, Show } from "solid-js";
import { useKeymapLayer } from "../../keymap/useKeymapLayer";
import { CIRCLE_FILLED, TRIANGLE_UP } from "../../shell/glyphs";
import { KeymapHintBar } from "../../shell/KeymapHintBar";
import { KitMarkdown } from "../../shell/KitMarkdown";
import { scrollbarStyle, theme } from "../../shell/theme";
import { WorkspacePanelLayout } from "../../shell/WorkspacePanelLayout";
import type { KitRelease } from "./release-check";
import type {
	ReleasesState,
	ReleasesWorkspaceController,
} from "./workspace-controller";

export const RELEASE_NOTES_MIN_COLS = 40;

type ScrollRef = {
	scrollBy: (options: { x: number; y: number }) => void;
	scrollTo: (options: { x: number; y: number }) => void;
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
	let wasActive = false;
	const active = () => props.active !== false;

	createEffect(() => {
		const isActive = active();
		if (isActive && !wasActive) scrollRef?.scrollTo({ x: 0, y: 0 });
		wasActive = isActive;
	});

	function releaseStatus(release: KitRelease): string | null {
		if (release.tag === state().latest?.tag) return "update";
		if (release.version === state().currentVersion) return "installed";
		return null;
	}

	async function openRelease(release: KitRelease): Promise<void> {
		try {
			await props.onOpenRelease(release.url);
		} catch (error) {
			props.onOpenError(error);
		}
	}

	async function openNewestRelease(): Promise<void> {
		const release = state().releases[0];
		if (release) await openRelease(release);
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
			"release-notes.open-latest": openNewestRelease,
			...(state().hasMore
				? {
						"release-notes.load-more": () =>
							props.controller.loadMoreReleases(),
					}
				: {}),
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
						<Show
							when={state().latest}
							fallback={
								<text fg={theme.textMuted}>v{state().currentVersion}</text>
							}
						>
							{(latest) => (
								<text fg={theme.warningText} wrapMode="none">
									{TRIANGLE_UP} v{latest().version} available
								</text>
							)}
						</Show>
					</box>
				}
				footer={<KeymapHintBar group="release-notes" borderless />}
			>
				<scrollbox
					ref={(value) => {
						scrollRef = value as ScrollRef;
					}}
					flexGrow={1}
					scrollY
					style={scrollbarStyle()}
				>
					<For each={state().releases}>
						{(release, index) => {
							const status = () => releaseStatus(release);
							return (
								<box
									paddingX={1}
									paddingY={1}
									border={index() > 0 ? ["top"] : undefined}
									borderColor={theme.borderDefault}
								>
									<box
										flexDirection="row"
										justifyContent="space-between"
										onMouseDown={(event) => {
											if (event.button !== 0) return;
											event.preventDefault();
											event.stopPropagation();
											props.onFocusRequest?.();
											void openRelease(release);
										}}
									>
										<box flexDirection="row" gap={1}>
											<text fg={theme.textPrimary}>
												<strong>v{release.version}</strong>
											</text>
											<Show when={status() === "update"}>
												<text fg={theme.warningText}>{TRIANGLE_UP} update</text>
											</Show>
											<Show when={status() === "installed"}>
												<text fg={theme.toolText}>
													{CIRCLE_FILLED} installed
												</text>
											</Show>
										</box>
										<Show when={release.publishedAt}>
											<text fg={theme.textMuted} wrapMode="none">
												{release.publishedAt?.slice(0, 10)}
											</text>
										</Show>
									</box>
									<box paddingTop={1}>
										<Show
											when={release.notes.trim()}
											fallback={
												<text fg={theme.textMuted}>
													No release notes were published for this version.
												</text>
											}
										>
											<KitMarkdown content={release.notes} />
										</Show>
									</box>
								</box>
							);
						}}
					</For>
					<Show when={state().hasMore}>
						<box
							paddingX={1}
							paddingY={1}
							border={["top"]}
							borderColor={theme.borderDefault}
							onMouseDown={(event) => {
								if (event.button !== 0 || state().loadingMore) return;
								event.preventDefault();
								event.stopPropagation();
								props.onFocusRequest?.();
								void props.controller.loadMoreReleases();
							}}
						>
							<text
								fg={state().loadingMore ? theme.textMuted : theme.textSecondary}
							>
								{state().loadingMore
									? "Loading more releases..."
									: "Load more releases"}
							</text>
						</box>
					</Show>
					<Show when={state().historyStatus === "loading"}>
						<box paddingX={1} paddingY={1}>
							<text fg={theme.textMuted}>Loading release history...</text>
						</box>
					</Show>
					<Show when={state().historyStatus === "unavailable"}>
						<box paddingX={1} paddingY={1}>
							<text fg={theme.textMuted}>Release history unavailable</text>
						</box>
					</Show>
				</scrollbox>
			</WorkspacePanelLayout>
		</box>
	);
}
