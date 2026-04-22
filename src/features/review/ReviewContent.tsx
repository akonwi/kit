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
import { syntaxStyle, theme } from "../../shell/theme";
import { loadReviewFiles, type ReviewFile } from "./model";

export type ReviewContentProps = {
	onClose: () => void;
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
	scrollTo: (position: number | { x: number; y: number }) => void;
};

export function ReviewContent(props: ReviewContentProps) {
	const [files] = createResource(() => loadReviewFiles());
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [selectedHunkIndex, setSelectedHunkIndex] = createSignal(0);
	const [expandedKeys, setExpandedKeys] = createSignal<Set<string>>(new Set());
	const [patchFocused, setPatchFocused] = createSignal(false);
	const patchScrollRefs = new Map<string, PatchScrollRef>();
	let listScrollRef: ScrollRef | undefined;

	const reviewFiles = createMemo(() => files() ?? []);
	const selectedFile = createMemo(() => reviewFiles()[selectedIndex()] ?? null);
	const selectedHunk = createMemo(
		() => selectedFile()?.hunks[selectedHunkIndex()] ?? null,
	);

	createEffect(() => {
		const list = reviewFiles();
		if (selectedIndex() >= list.length) {
			setSelectedIndex(Math.max(0, list.length - 1));
		}
	});

	createEffect(() => {
		const hunks = selectedFile()?.hunks ?? [];
		if (selectedHunkIndex() >= hunks.length) {
			setSelectedHunkIndex(Math.max(0, hunks.length - 1));
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

	function scrollToHunk(file: ReviewFile, hunkIndex: number) {
		const hunk = file.hunks[hunkIndex];
		if (!hunk) return;
		patchScrollRefs.get(file.id)?.scrollTo({
			x: 0,
			y: Math.max(0, hunk.patchStartLine - 2),
		});
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
				const nextIndex = Math.max(0, selectedHunkIndex() - 1);
				setSelectedHunkIndex(nextIndex);
				queueMicrotask(() => scrollToHunk(file, nextIndex));
				return;
			}
			if (e.name === "down" || e.name === "j") {
				e.preventDefault();
				const file = selectedFile();
				if (!file) return;
				const nextIndex = Math.min(
					file.hunks.length - 1,
					selectedHunkIndex() + 1,
				);
				setSelectedHunkIndex(nextIndex);
				queueMicrotask(() => scrollToHunk(file, nextIndex));
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
			setSelectedHunkIndex(0);
			return;
		}
		if (e.name === "down" || e.name === "j") {
			e.preventDefault();
			setSelectedIndex((index) =>
				Math.min(reviewFiles().length - 1, index + 1),
			);
			setSelectedHunkIndex(0);
			return;
		}
		if (e.name === "return" || e.name === "enter") {
			e.preventDefault();
			const file = selectedFile();
			if (file && expandedKeys().has(file.id)) {
				setPatchFocused(true);
				queueMicrotask(() => scrollToHunk(file, selectedHunkIndex()));
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
		<box
			position="absolute"
			left={0}
			top={0}
			width="100%"
			height="100%"
			zIndex={1200}
			backgroundColor={theme.modalBackdrop}
		>
			<box
				width="100%"
				height="100%"
				border
				borderStyle="double"
				borderColor={theme.borderFocused}
				backgroundColor={theme.bgSurface}
				padding={1}
				paddingBottom={0}
				flexDirection="column"
				gap={0}
			>
				<box
					flexShrink={0}
					flexDirection="row"
					justifyContent="space-between"
					paddingX={1}
					paddingBottom={1}
				>
					<text fg={theme.textMuted}>Code review</text>
					<text fg={theme.textMuted}>
						{reviewFiles().length} file{reviewFiles().length === 1 ? "" : "s"}
					</text>
				</box>

				<Show
					when={!files.loading}
					fallback={<text fg={theme.textMuted}>Loading diff…</text>}
				>
					<Show
						when={reviewFiles().length > 0}
						fallback={<text fg={theme.textMuted}>No uncommitted changes.</text>}
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
														border
														borderColor={
															selected()
																? theme.borderAccent
																: theme.borderDefault
														}
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
																	syntaxStyle={syntaxStyle}
																	showLineNumbers
																	addedBg="#16351f"
																	removedBg="#3a1f24"
																	contextBg={theme.bgSurface}
																	addedContentBg="#0f2917"
																	removedContentBg="#291217"
																	contextContentBg={theme.bgSurface}
																	addedSignColor={theme.toolText}
																	removedSignColor={theme.errorText}
																	lineNumberFg={theme.textMuted}
																	lineNumberBg={theme.bg}
																	addedLineNumberBg="#102717"
																	removedLineNumberBg="#2a1519"
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
									border
									borderColor={theme.borderAccent}
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
									<Show when={selectedHunk()}>
										{(hunk) => (
											<box paddingX={1} paddingBottom={1}>
												<text fg={theme.borderAccent}>
													{hunk().header}
													{hunk().context ? ` · ${hunk().context}` : ""}
												</text>
											</box>
										)}
									</Show>
									<Show when={selectedHunk()}>
										{(hunk) => (
											<box
												border
												borderColor={theme.borderAccent}
												paddingX={1}
												paddingY={0}
												marginBottom={1}
											>
												<text fg={theme.borderAccent}>
													{hunk().header}
													{hunk().context ? ` · ${hunk().context}` : ""}
												</text>
											</box>
										)}
									</Show>
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
												syntaxStyle={syntaxStyle}
												showLineNumbers
												addedBg="#16351f"
												removedBg="#3a1f24"
												contextBg={theme.bgSurface}
												addedContentBg="#0f2917"
												removedContentBg="#291217"
												contextContentBg={theme.bgSurface}
												addedSignColor={theme.toolText}
												removedSignColor={theme.errorText}
												lineNumberFg={theme.textMuted}
												lineNumberBg={theme.bg}
												addedLineNumberBg="#102717"
												removedLineNumberBg="#2a1519"
												wrapMode="none"
											/>
										</scrollbox>
									</box>
								</box>
							)}
						</Show>
					</Show>
				</Show>

				<box flexShrink={0}>
					<box
						border
						borderColor={theme.borderDefault}
						paddingX={1}
						paddingY={0}
					>
						<text fg={theme.textMuted}>
							{patchFocused()
								? "↑/↓ or j/k jump hunks · Esc back"
								: "↑/↓ or j/k move · Enter focus patch · Space collapse/expand · Esc close"}
						</text>
					</box>
				</box>
			</box>
		</box>
	);
}
