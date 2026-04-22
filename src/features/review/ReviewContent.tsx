import type { KeyEvent } from "@opentui/core";
import { useKeyboard } from "@opentui/solid";
import {
	createEffect,
	createMemo,
	createResource,
	createSignal,
	Show,
} from "solid-js";
import { syntaxStyle, theme } from "../../shell/theme";
import { loadReviewFiles, type ReviewFile } from "./model";

export type ReviewContentProps = {
	onClose: () => void;
	onSubmit: (message: string) => Promise<void>;
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

export function ReviewContent(_props: ReviewContentProps) {
	const [files] = createResource(loadReviewFiles);
	const [fileIndex, setFileIndex] = createSignal(0);
	const [hunkIndex, setHunkIndex] = createSignal(0);
	const [fileNotes] = createSignal(new Map<string, string>());
	const [hunkNotes] = createSignal(new Map<string, string>());
	let patchScrollRef:
		| {
				scrollBy: (delta: number | { x: number; y: number }) => void;
				scrollTo: (position: number | { x: number; y: number }) => void;
		  }
		| undefined;

	const reviewFiles = createMemo(() => files() ?? []);
	const selectedFile = createMemo(() => reviewFiles()[fileIndex()] ?? null);
	const selectedHunk = createMemo(
		() => selectedFile()?.hunks[hunkIndex()] ?? null,
	);
	const noteSummary = createMemo(() => {
		const fileCount = fileNotes().size;
		const hunkCount = hunkNotes().size;
		return `${fileCount} file note${fileCount === 1 ? "" : "s"} · ${hunkCount} hunk note${hunkCount === 1 ? "" : "s"}`;
	});

	createEffect(() => {
		const list = reviewFiles();
		if (fileIndex() >= list.length) {
			setFileIndex(Math.max(0, list.length - 1));
		}
	});

	createEffect(() => {
		const hunks = selectedFile()?.hunks ?? [];
		if (hunkIndex() >= hunks.length) {
			setHunkIndex(Math.max(0, hunks.length - 1));
		}
	});

	createEffect(() => {
		const hunk = selectedHunk();
		if (!hunk) return;
		patchScrollRef?.scrollTo({ x: 0, y: Math.max(0, hunk.patchStartLine - 3) });
	});

	useKeyboard((e: KeyEvent) => {
		if (e.name === "escape") {
			e.preventDefault();
			_props.onClose();
			return;
		}
		if (e.name === "up" || e.name === "k") {
			e.preventDefault();
			patchScrollRef?.scrollBy({ x: 0, y: -3 });
			return;
		}
		if (e.name === "down" || e.name === "j") {
			e.preventDefault();
			patchScrollRef?.scrollBy({ x: 0, y: 3 });
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
				flexDirection="column"
				gap={1}
			>
				<box
					flexShrink={0}
					flexDirection="row"
					justifyContent="space-between"
					paddingX={1}
					backgroundColor={theme.bg}
				>
					<text fg={theme.textMuted}>
						{selectedFile()
							? `${statusLabel(selectedFile() as ReviewFile)} ${selectedFile()?.path}`
							: "No uncommitted changes"}
					</text>
					<text fg={theme.textMuted}>{noteSummary()}</text>
				</box>

				<Show
					when={!files.loading}
					fallback={<text fg={theme.textMuted}>Loading diff…</text>}
				>
					<Show
						when={selectedFile()}
						fallback={<text fg={theme.textMuted}>No uncommitted changes.</text>}
					>
						{(file) => (
							<box flexGrow={1} flexDirection="column" gap={1}>
								<box
									flexDirection="row"
									justifyContent="space-between"
									paddingX={1}
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
										File {fileIndex() + 1}/{reviewFiles().length} · Hunk{" "}
										{Math.min(hunkIndex() + 1, file().hunks.length)}/
										{file().hunks.length} · {file().changeCount} changed line
										{file().changeCount === 1 ? "" : "s"}
									</text>
								</box>

								<box
									flexGrow={1}
									border
									borderColor={theme.borderAccent}
									padding={1}
									flexDirection="column"
									gap={1}
								>
									<box
										flexDirection="row"
										justifyContent="space-between"
										paddingX={1}
										backgroundColor={theme.bgMuted}
									>
										<text fg={theme.borderAccent}>Patch view</text>
										<text fg={theme.textMuted}>
											{file().filetype ?? "text"}
										</text>
									</box>
									<Show when={selectedHunk()}>
										{(hunk) => (
											<box paddingX={1}>
												<text fg={theme.borderAccent}>
													{hunk().header}
													{hunk().context ? ` · ${hunk().context}` : ""}
												</text>
											</box>
										)}
									</Show>
									<scrollbox
										ref={(value) => {
											patchScrollRef = value as typeof patchScrollRef;
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
			</box>
		</box>
	);
}
