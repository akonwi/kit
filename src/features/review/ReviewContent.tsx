import type { KeyEvent, PasteEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	For,
	Show,
} from "solid-js";
import { theme } from "../../shell/theme";
import { buildReviewFeedbackMessage } from "./feedback";
import { loadReviewFiles, type ReviewFile } from "./model";

export type ReviewContentProps = {
	onClose: () => void;
	onSubmit: (message: string) => Promise<void>;
};

type FocusArea = "files" | "hunks";
type Mode = "navigate" | "edit-file" | "edit-hunk";

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

export function ReviewContent(props: ReviewContentProps) {
	const [files, { refetch }] = createResource(loadReviewFiles);
	const [fileIndex, setFileIndex] = createSignal(0);
	const [hunkIndex, setHunkIndex] = createSignal(0);
	const [focusArea, setFocusArea] = createSignal<FocusArea>("files");
	const [mode, setMode] = createSignal<Mode>("navigate");
	const [draftNote, setDraftNote] = createSignal("");
	const [fileNotes, setFileNotes] = createSignal(new Map<string, string>());
	const [hunkNotes, setHunkNotes] = createSignal(new Map<string, string>());
	let textareaRef:
		| { plainText: string; setText: (value: string) => void }
		| undefined;

	const reviewFiles = createMemo(() => files() ?? []);
	const selectedFile = createMemo(() => reviewFiles()[fileIndex()] ?? null);
	const selectedHunk = createMemo(
		() => selectedFile()?.hunks[hunkIndex()] ?? null,
	);

	createEffect(() => {
		const list = reviewFiles();
		if (fileIndex() >= list.length) setFileIndex(Math.max(0, list.length - 1));
	});

	createEffect(() => {
		const hunks = selectedFile()?.hunks ?? [];
		if (hunkIndex() >= hunks.length)
			setHunkIndex(Math.max(0, hunks.length - 1));
	});

	createEffect(() => {
		const file = selectedFile();
		const hunk = selectedHunk();
		let next = "";
		if (mode() === "edit-file" && file) {
			next = fileNotes().get(file.path) ?? "";
		}
		if (mode() === "edit-hunk" && hunk) {
			next = hunkNotes().get(hunk.id) ?? "";
		}
		setDraftNote(next);
		try {
			textareaRef?.setText(next);
		} catch {
			textareaRef = undefined;
		}
	});

	function saveCurrentNote() {
		const text = draftNote().trim();
		const file = selectedFile();
		const hunk = selectedHunk();
		if (mode() === "edit-file" && file) {
			setFileNotes((prev) => {
				const next = new Map(prev);
				if (text) next.set(file.path, text);
				else next.delete(file.path);
				return next;
			});
		}
		if (mode() === "edit-hunk" && hunk) {
			setHunkNotes((prev) => {
				const next = new Map(prev);
				if (text) next.set(hunk.id, text);
				else next.delete(hunk.id);
				return next;
			});
		}
	}

	async function submitReview() {
		saveCurrentNote();
		const message = buildReviewFeedbackMessage({
			files: reviewFiles(),
			fileNotes: fileNotes(),
			hunkNotes: hunkNotes(),
		});
		if (!message) return;
		await props.onSubmit(message);
		props.onClose();
	}

	useKeyboard((e: KeyEvent) => {
		if (e.name === "escape") {
			e.preventDefault();
			if (mode() === "navigate") props.onClose();
			else {
				saveCurrentNote();
				setMode("navigate");
			}
			return;
		}
		if (mode() !== "navigate") {
			if (e.ctrl && e.name === "return") {
				e.preventDefault();
				void submitReview();
			}
			return;
		}
		if (e.name === "tab") {
			e.preventDefault();
			setFocusArea((current) => (current === "files" ? "hunks" : "files"));
			return;
		}
		if (e.name === "f") {
			e.preventDefault();
			setMode("edit-file");
			return;
		}
		if (e.name === "h") {
			e.preventDefault();
			if (selectedHunk()) setMode("edit-hunk");
			return;
		}
		if (e.name === "r") {
			e.preventDefault();
			void refetch();
			return;
		}
		if (e.ctrl && e.name === "return") {
			e.preventDefault();
			void submitReview();
			return;
		}
		if (e.name === "up" || e.name === "k") {
			e.preventDefault();
			if (focusArea() === "files") setFileIndex((i) => Math.max(0, i - 1));
			else setHunkIndex((i) => Math.max(0, i - 1));
			return;
		}
		if (e.name === "down" || e.name === "j") {
			e.preventDefault();
			if (focusArea() === "files") {
				setFileIndex((i) => Math.min(reviewFiles().length - 1, i + 1));
				setHunkIndex(0);
			} else {
				setHunkIndex((i) =>
					Math.min((selectedFile()?.hunks.length ?? 1) - 1, i + 1),
				);
			}
			return;
		}
	});

	function handlePaste(event: PasteEvent) {
		if (mode() === "navigate") return;
		const pasted = new TextDecoder()
			.decode(event.bytes)
			.replace(/\r\n/g, "\n")
			.replace(/\r/g, "\n");
		setDraftNote((current) => `${current}${pasted}`);
	}

	const noteSummary = createMemo(() => {
		const fileCount = fileNotes().size;
		const hunkCount = hunkNotes().size;
		return `${fileCount} file note${fileCount === 1 ? "" : "s"} · ${hunkCount} hunk note${hunkCount === 1 ? "" : "s"}`;
	});

	return (
		<box
			position="absolute"
			top={0}
			left={0}
			width="100%"
			height="100%"
			zIndex={1200}
			backgroundColor={theme.bg}
			flexDirection="column"
			border
			borderColor={theme.borderFocused}
		>
			<box
				flexShrink={0}
				flexDirection="row"
				justifyContent="space-between"
				paddingX={1}
				backgroundColor={theme.bgSurface}
			>
				<text fg={theme.textPrimary}>
					<b>Review</b>
				</text>
				<text fg={theme.textMuted}>{noteSummary()}</text>
			</box>
			<Show
				when={!files.loading}
				fallback={<text fg={theme.textMuted}>Loading diff…</text>}
			>
				<box flexGrow={1} flexDirection="row" gap={1} padding={1}>
					<box
						width="32%"
						border
						borderColor={
							focusArea() === "files"
								? theme.borderFocused
								: theme.borderDefault
						}
						padding={1}
						flexDirection="column"
					>
						<text fg={theme.textPrimary}>Files</text>
						<Show
							when={reviewFiles().length > 0}
							fallback={
								<text fg={theme.textMuted}>No uncommitted changes.</text>
							}
						>
							<For each={reviewFiles()}>
								{(file, idx) => {
									const selected = () => idx() === fileIndex();
									const hasFileNote = () => fileNotes().has(file.path);
									const hasHunkNote = () =>
										file.hunks.some((hunk) => hunkNotes().has(hunk.id));
									return (
										<box
											backgroundColor={
												selected() ? theme.pickerFocusedBg : theme.bgTransparent
											}
										>
											<text
												fg={
													selected()
														? theme.pickerFocusedText
														: theme.textPrimary
												}
											>
												{statusLabel(file)} {file.path}
											</text>
											<Show when={hasFileNote() || hasHunkNote()}>
												<text
													fg={
														selected()
															? theme.pickerFocusedText
															: theme.toolText
													}
												>
													{hasFileNote() ? "F" : ""}
													{hasHunkNote() ? "H" : ""}
												</text>
											</Show>
										</box>
									);
								}}
							</For>
						</Show>
					</box>
					<box
						flexGrow={1}
						border
						borderColor={
							focusArea() === "hunks"
								? theme.borderFocused
								: theme.borderDefault
						}
						padding={1}
						flexDirection="column"
						gap={1}
					>
						<text fg={theme.textPrimary}>Hunks</text>
						<Show
							when={selectedFile()}
							fallback={<text fg={theme.textMuted}>Select a file.</text>}
						>
							{(file) => (
								<>
									<text fg={theme.textMuted}>{file().path}</text>
									<Show
										when={file().hunks.length > 0}
										fallback={<text fg={theme.textMuted}>No hunks found.</text>}
									>
										<box flexDirection="column" gap={0}>
											<For each={file().hunks}>
												{(hunk, idx) => {
													const selected = () => idx() === hunkIndex();
													return (
														<box
															backgroundColor={
																selected()
																	? theme.pickerFocusedBg
																	: theme.bgTransparent
															}
															flexDirection="column"
														>
															<text
																fg={
																	selected()
																		? theme.pickerFocusedText
																		: theme.textPrimary
																}
															>
																{hunk.header}
															</text>
															<Show when={hunk.context}>
																<text
																	fg={
																		selected()
																			? theme.pickerFocusedText
																			: theme.textMuted
																	}
																>
																	{hunk.context}
																</text>
															</Show>
														</box>
													);
												}}
											</For>
										</box>
										<Show when={selectedHunk()}>
											{(hunk) => (
												<scrollbox flexGrow={1} scrollY paddingTop={1}>
													<box flexDirection="column">
														<For each={hunk().lines}>
															{(line) => (
																<text
																	fg={
																		line.kind === "add"
																			? theme.toolText
																			: line.kind === "delete"
																				? theme.errorText
																				: theme.textMuted
																	}
																>
																	{line.kind === "add"
																		? "+"
																		: line.kind === "delete"
																			? "-"
																			: " "}
																	{line.text}
																</text>
															)}
														</For>
													</box>
												</scrollbox>
											)}
										</Show>
									</Show>
								</>
							)}
						</Show>
					</box>
				</box>
			</Show>
			<box
				flexShrink={0}
				flexDirection="column"
				paddingX={1}
				paddingBottom={1}
				gap={0}
			>
				<text fg={theme.borderAccent}>
					{mode() === "edit-file"
						? `File note: ${selectedFile()?.path ?? ""}`
						: mode() === "edit-hunk"
							? `Hunk note: ${selectedHunk()?.header ?? ""}`
							: "Press f for file note, h for hunk note"}
				</text>
				<Show when={mode() !== "navigate"}>
					{/* @ts-ignore onPaste supported but not typed */}
					<textarea
						ref={(value) => {
							textareaRef = value as typeof textareaRef;
							try {
								textareaRef?.setText(draftNote());
							} catch {
								textareaRef = undefined;
							}
						}}
						minHeight={3}
						maxHeight={6}
						placeholder="Type your review note..."
						placeholderColor={theme.textPlaceholder}
						backgroundColor={theme.bg}
						focusedBackgroundColor={theme.bg}
						textColor={theme.textPrimary}
						focusedTextColor={theme.textPrimary}
						cursorColor={theme.cursor}
						showCursor
						wrapMode="word"
						focused
						keyBindings={[{ name: "return", shift: true, action: "newline" }]}
						onContentChange={() => setDraftNote(textareaRef?.plainText ?? "")}
						onPaste={handlePaste}
					/>
				</Show>
				<text fg={theme.textMuted}>
					{mode() === "navigate"
						? "↑/↓ navigate · Tab switch pane · f file note · h hunk note · r refresh · Ctrl+Enter submit · Esc close"
						: "Esc save note · Ctrl+Enter submit review"}
				</text>
			</box>
		</box>
	);
}
