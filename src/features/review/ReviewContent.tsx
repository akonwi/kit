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
import type { OverlayComponentProps } from "../../app/overlay-ui";
import type { AttachmentsController } from "../../shell/attachments-controller";
import { type Binding, HintBar } from "../../shell/HintBar";
import { ScreenHeader } from "../../shell/ScreenHeader";
import { ScreenLayout } from "../../shell/ScreenLayout";
import { syntaxStyle, theme } from "../../shell/theme";
import type { ToastInput } from "../../state/toasts";
import { CodeReviewAttachment } from "../code-review/attachment";
import {
	buildReviewSubmission,
	countDraftNotes,
	countFileDraftNotes,
	type ReviewDraftState,
} from "./draft";
import { loadReviewFiles, type ReviewFile, type ReviewHunk } from "./model";
import { ReviewNoteModal } from "./ReviewNoteModal";

export type ReviewContentProps = {
	onClose: () => void;
	attachments: AttachmentsController;
	toast: (toast: ToastInput) => void;
	openCustomOverlay: <T>(
		component: (
			props: OverlayComponentProps<T>,
		) => import("solid-js").JSX.Element,
	) => Promise<T>;
	surfaceProps?: OverlayComponentProps<void>["surfaceProps"];
};

const FOCUS_BINDINGS: { [key in "list" | "patch"]: Binding[] } = {
	list: [
		{ key: "↑/↓ or j/k", action: "move" },
		{ key: "Enter", action: "focus hunk" },
		{ key: "Space", action: "collapse/expand" },
		{ key: "f", action: "file note" },
		{ key: "x", action: "clear file note" },
		{ key: "s", action: "submit" },
		{ key: "Esc", action: "close" },
	],
	patch: [
		{ key: "↑/↓ or j/k", action: "scroll" },
		{ key: "←/→ or h/l", action: "change hunk" },
		{ key: "c", action: "hunk note" },
		{ key: "f", action: "file note" },
		{ key: "x", action: "clear hunk note" },
		{ key: "s", action: "submit" },
		{ key: "Esc", action: "back" },
	],
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

function formatNoteCount(count: number): string {
	return `${count} note${count === 1 ? "" : "s"}`;
}

function setMapValue(
	map: Map<string, string>,
	key: string,
	value: string,
): Map<string, string> {
	const next = new Map(map);
	if (value.trim().length === 0) next.delete(key);
	else next.set(key, value);
	return next;
}

export function ReviewContent(props: ReviewContentProps) {
	const [files] = createResource(() => loadReviewFiles());
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [expandedKeys, setExpandedKeys] = createSignal<Set<string>>(new Set());
	const [patchFocused, setPatchFocused] = createSignal(false);
	const [fileNotes, setFileNotes] = createSignal<Map<string, string>>(
		new Map(),
	);
	const [hunkNotes, setHunkNotes] = createSignal<Map<string, string>>(
		new Map(),
	);
	const [selectedHunkIndices, setSelectedHunkIndices] = createSignal<
		Map<string, number>
	>(new Map());
	const [editorOpen, setEditorOpen] = createSignal(false);
	const patchScrollRefs = new Map<string, PatchScrollRef>();
	let listScrollRef: ScrollRef | undefined;

	const reviewFiles = createMemo(() => files() ?? []);
	const draftState = createMemo<ReviewDraftState>(() => ({
		fileNotes: fileNotes(),
		hunkNotes: hunkNotes(),
	}));
	const totalDraftNotes = createMemo(() => countDraftNotes(draftState()));
	const selectedFile = createMemo(() => reviewFiles()[selectedIndex()] ?? null);
	const selectedHunk = createMemo(() => {
		const file = selectedFile();
		if (!file || file.hunks.length === 0) return null;
		const index = Math.max(
			0,
			Math.min(file.hunks.length - 1, selectedHunkIndices().get(file.id) ?? 0),
		);
		return file.hunks[index] ?? null;
	});
	const selectedHunkIndex = createMemo(() => {
		const file = selectedFile();
		const hunk = selectedHunk();
		if (!file || !hunk) return -1;
		return file.hunks.findIndex((candidate) => candidate.id === hunk.id);
	});

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

	function selectedFileNote(file: ReviewFile): string {
		return fileNotes().get(file.noteKey)?.trim() ?? "";
	}

	function selectedHunkNote(hunk: ReviewHunk): string {
		return hunkNotes().get(hunk.noteKey)?.trim() ?? "";
	}

	function setSelectedHunkIndex(fileId: string, index: number) {
		setSelectedHunkIndices((prev) => {
			const next = new Map(prev);
			next.set(fileId, index);
			return next;
		});
	}

	function cycleHunk(delta: number) {
		const file = selectedFile();
		if (!file || file.hunks.length === 0) return;
		const current = selectedHunkIndex();
		const nextIndex = Math.max(
			0,
			Math.min(file.hunks.length - 1, current + delta),
		);
		setSelectedHunkIndex(file.id, nextIndex);
	}

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

	async function openFileNoteEditor(file: ReviewFile) {
		setEditorOpen(true);
		try {
			const nextValue = await props.openCustomOverlay<string | null>(
				(overlayProps) => (
					<ReviewNoteModal
						surfaceProps={overlayProps.surfaceProps}
						title={`File note · ${file.path}`}
						subtitle={file.prevPath ? `from ${file.prevPath}` : undefined}
						initialValue={selectedFileNote(file)}
						placeholder="Comment on the whole file..."
						onClose={overlayProps.done}
					/>
				),
			);
			if (nextValue === null) return;
			setFileNotes((prev) => setMapValue(prev, file.noteKey, nextValue));
		} finally {
			setEditorOpen(false);
		}
	}

	async function openHunkNoteEditor(file: ReviewFile, hunk: ReviewHunk) {
		setEditorOpen(true);
		try {
			const nextValue = await props.openCustomOverlay<string | null>(
				(overlayProps) => (
					<ReviewNoteModal
						surfaceProps={overlayProps.surfaceProps}
						title={`Hunk note · ${file.path}`}
						subtitle={hunk.header}
						initialValue={selectedHunkNote(hunk)}
						placeholder="Comment on the selected hunk..."
						onClose={overlayProps.done}
					/>
				),
			);
			if (nextValue === null) return;
			setHunkNotes((prev) => setMapValue(prev, hunk.noteKey, nextValue));
		} finally {
			setEditorOpen(false);
		}
	}

	function clearSelectedFileNote() {
		const file = selectedFile();
		if (!file) return;
		setFileNotes((prev) => setMapValue(prev, file.noteKey, ""));
	}

	function clearSelectedHunkNote() {
		const hunk = selectedHunk();
		if (!hunk) return;
		setHunkNotes((prev) => setMapValue(prev, hunk.noteKey, ""));
	}

	function submitReview() {
		const submission = buildReviewSubmission(reviewFiles(), draftState());
		if (!submission) {
			props.toast({
				title: "No review notes",
				lines: ["Add a file or hunk note before submitting review."],
				variant: "warning",
			});
			return;
		}
		props.attachments.attach(
			new CodeReviewAttachment("code-review", submission),
		);
		props.toast({
			title: "Code review attached",
			lines: [
				`Attached ${formatNoteCount(totalDraftNotes())} across ${submission.files.length} file${submission.files.length === 1 ? "" : "s"}.`,
			],
			variant: "info",
		});
		props.onClose();
	}

	useKeyboard((e: KeyEvent) => {
		if (editorOpen()) return;
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
			if (e.name === "left" || e.name === "h") {
				e.preventDefault();
				cycleHunk(-1);
				return;
			}
			if (e.name === "right" || e.name === "l") {
				e.preventDefault();
				cycleHunk(1);
				return;
			}
			if (e.name === "c") {
				e.preventDefault();
				const file = selectedFile();
				const hunk = selectedHunk();
				if (!file || !hunk) return;
				void openHunkNoteEditor(file, hunk);
				return;
			}
			if (e.name === "f") {
				e.preventDefault();
				const file = selectedFile();
				if (!file) return;
				void openFileNoteEditor(file);
				return;
			}
			if (e.name === "x") {
				e.preventDefault();
				clearSelectedHunkNote();
				return;
			}
			if (e.name === "s") {
				e.preventDefault();
				submitReview();
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
				if (file.hunks.length > 0) {
					setSelectedHunkIndex(
						file.id,
						selectedHunkIndices().get(file.id) ?? 0,
					);
				}
			}
			return;
		}
		if (e.name === "space") {
			e.preventDefault();
			const file = selectedFile();
			if (file) toggleExpanded(file.id);
			return;
		}
		if (e.name === "f") {
			e.preventDefault();
			const file = selectedFile();
			if (!file) return;
			void openFileNoteEditor(file);
			return;
		}
		if (e.name === "x") {
			e.preventDefault();
			clearSelectedFileNote();
			return;
		}
		if (e.name === "s") {
			e.preventDefault();
			submitReview();
		}
	});

	return (
		<ScreenLayout
			surfaceProps={props.surfaceProps}
			zIndex={1200}
			header={
				<ScreenHeader
					left={<text fg={theme.textMuted}>Review diff</text>}
					right={
						<text fg={theme.textMuted}>
							{reviewFiles().length} file{reviewFiles().length === 1 ? "" : "s"}
							{totalDraftNotes() > 0
								? ` · ${formatNoteCount(totalDraftNotes())}`
								: ""}
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
											const noteCount = () =>
												countFileDraftNotes(file, draftState());
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
																{noteCount() > 0
																	? ` · ✎ ${formatNoteCount(noteCount())}`
																	: ""}
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
																removedLineNumberBg={
																	theme.diffRemovedLineNumberBg
																}
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
						{(file) => {
							const currentHunk = createMemo(() => selectedHunk());
							const hunkNote = createMemo(() => {
								const hunk = currentHunk();
								return hunk ? selectedHunkNote(hunk) : "";
							});
							const fileNote = createMemo(() => selectedFileNote(file()));
							return (
								<box
									flexGrow={1}
									flexDirection="column"
									gap={1}
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
												{statusLabel(file())} {file().path}
											</text>
											<Show when={file().prevPath}>
												<text fg={theme.textMuted}>from {file().prevPath}</text>
											</Show>
										</box>
										<text fg={theme.textMuted}>
											{currentHunk()
												? `change group ${selectedHunkIndex() + 1}/${file().hunks.length}`
												: `${file().hunks.length} hunk${file().hunks.length === 1 ? "" : "s"}`}
										</text>
									</box>

									<Show when={fileNote().length > 0 || hunkNote().length > 0}>
										<box
											flexShrink={0}
											marginX={1}
											border
											borderColor={theme.borderDefault}
											paddingX={1}
											flexDirection="column"
											gap={0}
										>
											<Show when={fileNote().length > 0}>
												<text fg={theme.textPrimary}>
													File note: {fileNote()}
												</text>
											</Show>
											<Show when={hunkNote().length > 0}>
												<text fg={theme.textPrimary}>
													Hunk note: {hunkNote()}
												</text>
											</Show>
										</box>
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
												diff={currentHunk()?.rawPatch ?? file().rawPatch}
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
							);
						}}
					</Show>
				</Show>
			</Show>
		</ScreenLayout>
	);
}
