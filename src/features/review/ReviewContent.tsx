import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	For,
	Show,
} from "solid-js";
import { type Binding, HintBar } from "../../shell/HintBar";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { syntaxStyle, theme } from "../../shell/theme";
import { loadReviewFiles, type ReviewFile } from "./model";

export type ReviewContentProps = {
	onClose: () => void;
};

const FOCUS_BINDINGS: { [key in "list" | "patch"]: Binding[] } = {
	list: [{ key: "↑/↓ or j/k", action: "move" }, { key: "Enter", action: "focus patch" }, { key: "Space", action: "collapse/expand" }, { key: "Esc", action: "close" }],
	patch: [{ key: "↑/↓ or j/k", action: "scroll" }, { key: "Esc", action: "back" }],
};

function statusLabel(file: ReviewFile): string {
	switch (file.status) {
		case "new":
			return "A";
		case "deleted":
			return "D";
		case "rename-pure":
		case "rename-changed":
			return "R";
		default:
			return "M";
	}
}

type ScrollRef = {
	scrollChildIntoView: (childId: string) => void;
	scrollBy: (delta: number | { x: number; y: number }) => void;
};

type PatchScrollRef = {
	scrollBy: (delta: number | { x: number; y: number }) => void;
};

export function ReviewContent(props: ReviewContentProps) {
	const [files] = createResource(() => loadReviewFiles());
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [expandedKeys, setExpandedKeys] = createSignal<Set<string>>(new Set());
	const [patchFocused, setPatchFocused] = createSignal(false);
	const patchScrollRefs = new Map<string, PatchScrollRef>();
	let listScrollRef: ScrollRef | undefined;

	const reviewFiles = createMemo(() => files() ?? []);
	const selectedFile = createMemo(() => reviewFiles()[selectedIndex()] ?? null);

	createEffect(() => {
		const list = reviewFiles();
		if (selectedIndex() >= list.length) {
			setSelectedIndex(Math.max(0, list.length - 1));
		}
	});


	createEffect(() => {
		const list = reviewFiles();
		if (list.length === 0) {
			setExpandedKeys(new Set<string>());
			setPatchFocused(false);
			return;
		}
		setExpandedKeys((prev) => {
			if (prev.size > 0) return prev;
			return new Set<string>(list.map((file) => file.id));
		});
	});

	createEffect(() => {
		if (patchFocused()) return;
		const file = selectedFile();
		if (!file) return;
		listScrollRef?.scrollChildIntoView(`review-file-${file.id}`);
	});

	function toggleExpanded(fileId: string) {
		setExpandedKeys((prev) => {
			const next = new Set(prev);
			if (next.has(fileId)) {
				next.delete(fileId);
				if (selectedFile()?.id === fileId) setPatchFocused(false);
			} else {
				next.add(fileId);
			}
			return next;
		});
	}

	function scrollPatch(fileId: string, deltaY: number) {
		patchScrollRefs.get(fileId)?.scrollBy({ x: 0, y: deltaY });
	}

	useKeyboard((e: KeyEvent) => {
		if (patchFocused()) {
			if (e.name === "escape") {
				e.preventDefault();
				setPatchFocused(false);
				return;
			}
			if (e.name === "up" || e.name === "k") {
				e.preventDefault();
				const file = selectedFile();
				if (!file) return;
				scrollPatch(file.id, -1);
				return;
			}
			if (e.name === "down" || e.name === "j") {
				e.preventDefault();
				const file = selectedFile();
				if (!file) return;
				scrollPatch(file.id, 1);
				return;
			}
			return;
		}

		if (e.name === "escape") {
			e.preventDefault();
			props.onClose();
			return;
		}
		if (e.name === "up" || e.name === "k") {
			e.preventDefault();
			setSelectedIndex((index) => Math.max(0, index - 1));

			return;
		}
		if (e.name === "down" || e.name === "j") {
			e.preventDefault();
			setSelectedIndex((index) =>
				Math.min(reviewFiles().length - 1, index + 1),
			);

			return;
		}
		if (e.name === "return" || e.name === "enter") {
			e.preventDefault();
			const file = selectedFile();
			if (file && expandedKeys().has(file.id)) {
				setPatchFocused(true);
			}
			return;
		}
		if (e.name === "space") {
			e.preventDefault();
			const file = selectedFile();
			if (file) toggleExpanded(file.id);
		}
	});

	return (
		<ScreenLayout
			zIndex={1200}
			header={
				<ScreenHeader
					left={<text fg={theme.textMuted}>Diffs</text>}
					right={
						<text fg={theme.textMuted}>
							{reviewFiles().length} file{reviewFiles().length === 1 ? "" : "s"}
						</text>
					}
				/>
			}
			footer={
				<HintBar bindings={FOCUS_BINDINGS[patchFocused() ? "patch" : "list"]} />
			}
		>
			<Show
				when={!files.loading}
				fallback={
					<box flexGrow={1} padding={1}>
						<text fg={theme.textMuted}>Loading diff…</text>
					</box>
				}
			>
				<Show
					when={reviewFiles().length > 0}
					fallback={
						<box flexGrow={1} padding={1}>
							<text fg={theme.textMuted}>No uncommitted changes.</text>
						</box>
					}
				>
					<Show
						when={patchFocused() && selectedFile()}
						fallback={
							<scrollbox
								ref={(value) => {
									listScrollRef = value as ScrollRef;
								}}
								flexGrow={1}
								scrollY
								padding={1}
							>
								<box flexDirection="column" gap={0}>
									<For each={reviewFiles()}>
										{(file, idx) => {
											const selected = () => idx() === selectedIndex();
											const expanded = () => expandedKeys().has(file.id);
											return (
												<box
													id={`review-file-${file.id}`}
													flexDirection="column"
													gap={0}
													backgroundColor={
														selected() ? theme.bgMuted : theme.bgTransparent
													}
												>
													<box
														paddingX={1}
														paddingY={0}
														flexDirection="row"
														justifyContent="space-between"
													>
														<box flexDirection="column">
															<text
																fg={
																	selected()
																		? theme.textPrimary
																		: theme.textSecondary
																}
															>
																{expanded() ? "▾" : "▸"} {statusLabel(file)}{" "}
																{file.path}
															</text>
															<Show when={file.prevPath}>
																<text fg={theme.textMuted}>
																	from {file.prevPath}
																</text>
															</Show>
														</box>
														<text fg={theme.textMuted}>
															{file.hunks.length} hunk
															{file.hunks.length === 1 ? "" : "s"} ·{" "}
															{file.changeCount} changed line
															{file.changeCount === 1 ? "" : "s"}
														</text>
													</box>
													<Show when={expanded()}>
														<box
															padding={1}
															paddingTop={0}
															flexDirection="column"
															gap={0}
														>
															<diff
																diff={file.rawPatch}
																view="unified"
																filetype={file.filetype}
																syntaxStyle={syntaxStyle()}
																showLineNumbers
																addedBg={theme.diffAddedBg}
																removedBg={theme.diffRemovedBg}
																contextBg={theme.bgSurface}
																addedContentBg={theme.diffAddedContentBg}
																removedContentBg={theme.diffRemovedContentBg}
																contextContentBg={theme.bgSurface}
																addedSignColor={theme.toolText}
																removedSignColor={theme.errorText}
																lineNumberFg={theme.textMuted}
																lineNumberBg={theme.bg}
																addedLineNumberBg={theme.diffAddedLineNumberBg}
																removedLineNumberBg={theme.diffRemovedLineNumberBg}
																wrapMode="none"
															/>
														</box>
													</Show>
												</box>
											);
										}}
									</For>
								</box>
							</scrollbox>
						}
					>
						{(file) => (
							<box
								flexGrow={1}
								flexDirection="column"
								gap={0}
								backgroundColor={theme.bgMuted}
							>
								<box
									flexShrink={0}
									paddingX={1}
									paddingY={0}
									flexDirection="row"
									justifyContent="space-between"
								>
									<box flexDirection="column">
										<text fg={theme.textPrimary}>
											▾ {statusLabel(file())} {file().path}
										</text>
										<Show when={file().prevPath}>
											<text fg={theme.textMuted}>from {file().prevPath}</text>
										</Show>
									</box>
									<text fg={theme.textMuted}>
										{file().hunks.length} hunk
										{file().hunks.length === 1 ? "" : "s"} ·{" "}
										{file().changeCount} changed line
										{file().changeCount === 1 ? "" : "s"}
									</text>
								</box>
								<box
									flexGrow={1}
									padding={1}
									paddingTop={0}
									backgroundColor={theme.bg}
								>
									<scrollbox
										ref={(value) => {
											if (value)
												patchScrollRefs.set(
													file().id,
													value as PatchScrollRef,
												);
										}}
										flexGrow={1}
										scrollY
									>
										<diff
											diff={file().rawPatch}
											view="unified"
											filetype={file().filetype}
											syntaxStyle={syntaxStyle()}
											showLineNumbers
											addedBg={theme.diffAddedBg}
											removedBg={theme.diffRemovedBg}
											contextBg={theme.bgSurface}
											addedContentBg={theme.diffAddedContentBg}
											removedContentBg={theme.diffRemovedContentBg}
											contextContentBg={theme.bgSurface}
											addedSignColor={theme.toolText}
											removedSignColor={theme.errorText}
											lineNumberFg={theme.textMuted}
											lineNumberBg={theme.bg}
											addedLineNumberBg={theme.diffAddedLineNumberBg}
											removedLineNumberBg={theme.diffRemovedLineNumberBg}
											wrapMode="none"
										/>
									</scrollbox>
								</box>
							</box>
						)}
					</Show>
				</Show>
			</Show>

		</ScreenLayout>
	);
}
