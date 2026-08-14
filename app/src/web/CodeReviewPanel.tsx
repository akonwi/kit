/** @jsxImportSource solid-js */
import {
	createEffect,
	createMemo,
	createSignal,
	For,
	type JSX,
	onCleanup,
	onMount,
	Show,
} from "solid-js";
import { useCodeReview } from "./CodeReviewProvider";
import {
	PierreDiff,
	type ReviewDiffLayout,
	type ReviewDiffOverflow,
} from "./PierreDiff";
import { WebIcon } from "./WebIcon";

const REVIEW_LAYOUT_KEY = "kit.web.codeReview.layout";
const REVIEW_WRAP_KEY = "kit.web.codeReview.wrap";

function readLayout(): ReviewDiffLayout {
	return localStorage.getItem(REVIEW_LAYOUT_KEY) === "split"
		? "split"
		: "unified";
}

function readOverflow(): ReviewDiffOverflow {
	return localStorage.getItem(REVIEW_WRAP_KEY) === "true" ? "wrap" : "scroll";
}

function statusLabel(status: string): string {
	if (status === "add") return "A";
	if (status === "delete") return "D";
	if (status === "rename") return "R";
	if (status === "copy") return "C";
	return "M";
}

export function CodeReviewPanel(): JSX.Element {
	const review = useCodeReview();
	const [mobileFilesOpen, setMobileFilesOpen] = createSignal(false);
	const [layout, setLayout] = createSignal<ReviewDiffLayout>(readLayout());
	const [overflow, setOverflow] = createSignal<ReviewDiffOverflow>(
		readOverflow(),
	);
	const [viewMenuOpen, setViewMenuOpen] = createSignal(false);
	const currentPath = createMemo(() => review.selectedFile()?.file.path);
	const currentNotes = createMemo(() =>
		review.notes().filter((note) => note.path === currentPath()),
	);
	const currentDraft = createMemo(() => {
		const value = review.draft();
		return value?.path === currentPath() ? value : null;
	});
	let viewMenu: HTMLDivElement | undefined;

	createEffect(() => localStorage.setItem(REVIEW_LAYOUT_KEY, layout()));
	createEffect(() =>
		localStorage.setItem(REVIEW_WRAP_KEY, String(overflow() === "wrap")),
	);

	onMount(() => {
		const closeMenu = (event: PointerEvent): void => {
			if (
				viewMenuOpen() &&
				event.target instanceof Node &&
				!viewMenu?.contains(event.target)
			) {
				setViewMenuOpen(false);
			}
		};
		const closeMenuWithEscape = (event: KeyboardEvent): void => {
			if (event.key !== "Escape" || !viewMenuOpen()) return;
			setViewMenuOpen(false);
			viewMenu?.querySelector<HTMLElement>("button")?.focus();
		};
		document.addEventListener("pointerdown", closeMenu);
		document.addEventListener("keydown", closeMenuWithEscape);
		onCleanup(() => {
			document.removeEventListener("pointerdown", closeMenu);
			document.removeEventListener("keydown", closeMenuWithEscape);
		});
	});

	return (
		<section class="code-review-panel" aria-label="Code review">
			<header class="workspace-panel-header">
				<strong>Code review</strong>
				<span>
					{`${review.state()?.files.length ?? 0} files · ${review.notes().length} ${review.notes().length === 1 ? "note" : "notes"}`}
				</span>
				<div ref={viewMenu} class="code-review-view-menu">
					<button
						type="button"
						data-variant="ghost"
						class="code-review-view-trigger"
						aria-label="Review display settings"
						aria-expanded={viewMenuOpen()}
						onClick={() => setViewMenuOpen((open) => !open)}
					>
						<WebIcon name="settings" />
					</button>
					<Show when={viewMenuOpen()}>
						<div class="code-review-view-menu-content">
							<fieldset>
								<legend>Layout</legend>
								<label>
									<input
										type="radio"
										name="code-review-layout"
										checked={layout() === "unified"}
										onChange={() => setLayout("unified")}
									/>
									Unified
								</label>
								<label>
									<input
										type="radio"
										name="code-review-layout"
										checked={layout() === "split"}
										onChange={() => setLayout("split")}
									/>
									Split
								</label>
							</fieldset>
							<label class="code-review-view-toggle">
								<input
									type="checkbox"
									checked={overflow() === "wrap"}
									onChange={(event) =>
										setOverflow(event.currentTarget.checked ? "wrap" : "scroll")
									}
								/>
								Wrap lines
							</label>
						</div>
					</Show>
				</div>
				<button
					type="button"
					data-variant="ghost"
					onClick={review.close}
					aria-label="Close code review"
				>
					×
				</button>
			</header>
			<Show when={review.error()}>
				<div class="code-review-error" role="alert">
					{review.error()}
				</div>
			</Show>
			<div
				class="code-review-body"
				classList={{ "is-mobile-files-open": mobileFilesOpen() }}
			>
				<nav class="code-review-files" aria-label="Changed files">
					<div class="code-review-files-header">
						<strong>Changed files</strong>
						<span>{review.state()?.files.length ?? 0}</span>
					</div>
					<For each={review.state()?.files ?? []}>
						{(file) => (
							<button
								type="button"
								class="code-review-file"
								classList={{ "is-active": file.path === currentPath() }}
								disabled={
									review.draft() !== null && file.path !== currentPath()
								}
								title={
									review.draft() !== null && file.path !== currentPath()
										? "Add or cancel the current note first"
										: file.path
								}
								onClick={() => {
									setMobileFilesOpen(false);
									void review.selectFile(file.path);
								}}
							>
								<span class="code-review-status">
									{statusLabel(file.status)}
								</span>
								<span class="code-review-path">{file.path}</span>
								<span class="code-review-counts">
									+{file.additions} −{file.deletions}
								</span>
								<Show
									when={
										review.notes().filter((note) => note.path === file.path)
											.length
									}
								>
									{(count) => (
										<span class="code-review-note-count">{count()}</span>
									)}
								</Show>
							</button>
						)}
					</For>
				</nav>
				<main class="code-review-diff">
					<Show
						when={review.selectedFile()}
						fallback={
							<div class="code-review-empty">No uncommitted changes</div>
						}
					>
						{(selected) => (
							<>
								<div class="code-review-file-header">
									<button
										type="button"
										data-variant="ghost"
										class="code-review-files-trigger"
										onClick={() => setMobileFilesOpen(true)}
									>
										Files
									</button>
									<strong title={selected().file.path}>
										{selected().file.path}
									</strong>
								</div>
								<div class="code-review-scroll">
									<PierreDiff
										file={selected()}
										layout={layout()}
										overflow={overflow()}
										notes={currentNotes()}
										draft={currentDraft()}
										onSelectRange={(range) => {
											const path = currentPath();
											if (path) review.selectRange(path, range);
										}}
										onSaveNote={(_, comment) => {
											const value = currentDraft();
											if (value) review.saveNote(value, comment);
										}}
										onEditNote={(note) => {
											const value = review
												.notes()
												.find((candidate) => candidate.id === note.id);
											if (value) review.editNote(value);
										}}
										onDraftChange={review.updateDraftComment}
										onDeleteNote={review.deleteNote}
										onCancelNote={review.cancelNote}
									/>
								</div>
							</>
						)}
					</Show>
				</main>
			</div>
		</section>
	);
}
