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
import { scoreMatch } from "../features/files/score";
import { DialogFrame } from "./DialogFrame";
import { OverlayHintBar } from "./OverlayHintBar";
import type { RemoteCommand } from "./remote-services";
import { useWebClient } from "./WebClientContext";

export function CommandPalette(): JSX.Element {
	const { controller, snapshot } = useWebClient();
	let input: HTMLInputElement | undefined;
	let loadGeneration = 0;
	const [open, setOpen] = createSignal(false);
	const [commands, setCommands] = createSignal<RemoteCommand[]>([]);
	const [registryGeneration, setRegistryGeneration] = createSignal<
		number | null
	>(null);
	const [query, setQuery] = createSignal("");
	const [selectedIndex, setSelectedIndex] = createSignal(0);
	const [loading, setLoading] = createSignal(false);
	const [error, setError] = createSignal("");
	const [argumentCommand, setArgumentCommand] =
		createSignal<RemoteCommand | null>(null);

	const filteredCommands = createMemo(() => {
		const needle = query().trim();
		if (!needle) return commands();
		return commands()
			.map((command) => ({
				command,
				score: Math.max(
					scoreMatch(command.name, needle),
					scoreMatch(command.description ?? "", needle),
				),
			}))
			.filter((entry) => entry.score > 0)
			.sort((a, b) => b.score - a.score)
			.map((entry) => entry.command);
	});
	const selectedCommand = createMemo(
		() => filteredCommands()[selectedIndex()] ?? null,
	);

	createEffect(() => {
		const count = filteredCommands().length;
		setSelectedIndex((index) =>
			count === 0 ? 0 : Math.max(0, Math.min(index, count - 1)),
		);
	});

	let observedStreamId: string | null = null;
	createEffect(() => {
		const protocol = snapshot().protocol;
		if (
			open() &&
			(protocol.phase !== "live" ||
				(observedStreamId !== null && protocol.streamId !== observedStreamId))
		) {
			closePalette();
		}
		observedStreamId = protocol.streamId;
	});

	async function loadCommands(): Promise<void> {
		const generation = ++loadGeneration;
		setLoading(true);
		setError("");
		setCommands([]);
		setRegistryGeneration(null);
		try {
			const next = await controller.listCommands();
			if (generation !== loadGeneration || !open()) return;
			setCommands(next.commands);
			setRegistryGeneration(next.registryGeneration);
		} catch (cause) {
			if (generation !== loadGeneration || !open()) return;
			setError(cause instanceof Error ? cause.message : String(cause));
		} finally {
			if (generation === loadGeneration) setLoading(false);
		}
	}

	function openPalette(): void {
		const modal = document.querySelector<HTMLDialogElement>("dialog:modal");
		if (modal && modal.id !== "command-palette") return;
		setQuery("");
		setSelectedIndex(0);
		setArgumentCommand(null);
		setOpen(true);
		void loadCommands();
	}

	function closePalette(): void {
		loadGeneration += 1;
		setOpen(false);
		setArgumentCommand(null);
		setRegistryGeneration(null);
		setError("");
	}

	function cancelPalette(): void {
		closePalette();
	}

	function scrollSelectedCommandIntoView(): void {
		queueMicrotask(() =>
			document
				.getElementById(`command-option-${selectedIndex()}`)
				?.scrollIntoView({ block: "nearest" }),
		);
	}

	function moveSelection(delta: -1 | 1): void {
		const count = filteredCommands().length;
		if (count === 0) return;
		setSelectedIndex((index) => (index + delta + count) % count);
		scrollSelectedCommandIntoView();
	}

	function chooseCommand(command: RemoteCommand): void {
		const generation = registryGeneration();
		if (generation === null) return;
		if (command.argName) {
			setArgumentCommand(command);
			setQuery("");
			queueMicrotask(() => input?.focus());
			return;
		}
		closePalette();
		void controller.executeCommand(command.id, "", generation);
	}

	function runArgumentCommand(): void {
		const command = argumentCommand();
		const generation = registryGeneration();
		if (!command || generation === null) return;
		const args = query();
		closePalette();
		void controller.executeCommand(command.id, args, generation);
	}

	function handleInputKeyDown(event: KeyboardEvent): void {
		if (event.key === "ArrowDown") {
			event.preventDefault();
			moveSelection(1);
		} else if (event.key === "ArrowUp") {
			event.preventDefault();
			moveSelection(-1);
		} else if (event.key === "Enter") {
			event.preventDefault();
			if (argumentCommand()) runArgumentCommand();
			else {
				const command = selectedCommand();
				if (command) chooseCommand(command);
			}
		}
	}

	onMount(() => {
		const handleGlobalKeyDown = (event: KeyboardEvent) => {
			if (
				event.defaultPrevented ||
				!event.ctrlKey ||
				event.altKey ||
				event.shiftKey ||
				event.key.toLowerCase() !== "p"
			) {
				return;
			}
			event.preventDefault();
			if (open()) input?.focus();
			else openPalette();
		};
		window.addEventListener("keydown", handleGlobalKeyDown);
		onCleanup(() => window.removeEventListener("keydown", handleGlobalKeyDown));
	});

	return (
		<DialogFrame
			open={open()}
			id="command-palette"
			class="command-palette-dialog"
			labelledBy="command-palette-title"
			focusKey={argumentCommand()?.id ?? "commands"}
			onAfterOpen={() => input?.focus()}
			onCancel={cancelPalette}
		>
			<header>
				<h2 id="command-palette-title" data-visually-hidden>
					Command palette
				</h2>
				<label class="command-palette-input">
					<span aria-hidden="true">&gt;</span>
					<input
						ref={input}
						type="text"
						value={query()}
						placeholder={
							argumentCommand()?.argName
								? `Enter ${argumentCommand()?.argName}`
								: "Search commands"
						}
						aria-label={
							argumentCommand()?.argName
								? `Arguments for ${argumentCommand()?.name}`
								: "Search commands"
						}
						role={argumentCommand() ? undefined : "combobox"}
						aria-autocomplete={argumentCommand() ? undefined : "list"}
						aria-expanded={argumentCommand() ? undefined : true}
						aria-controls={
							argumentCommand() ? undefined : "command-palette-list"
						}
						aria-activedescendant={
							argumentCommand() || !selectedCommand()
								? undefined
								: `command-option-${selectedIndex()}`
						}
						onInput={(event) => {
							setQuery(event.currentTarget.value);
							setSelectedIndex(0);
							scrollSelectedCommandIntoView();
						}}
						onKeyDown={handleInputKeyDown}
					/>
				</label>
			</header>
			<div class="command-palette-body">
				<Show
					when={!argumentCommand()}
					fallback={
						<div class="command-argument-help">
							<strong>{argumentCommand()?.name}</strong>
							<Show when={argumentCommand()?.description}>
								{(description) => <span>{description()}</span>}
							</Show>
						</div>
					}
				>
					<Show when={error()}>
						{(message) => (
							<div class="command-palette-message is-error" role="alert">
								{message()}
							</div>
						)}
					</Show>
					<Show when={!error() && filteredCommands().length === 0}>
						<div
							class="command-palette-message"
							role="status"
							aria-live="polite"
						>
							{loading() ? "Loading…" : "No results"}
						</div>
					</Show>
					<div id="command-palette-list" role="listbox" aria-label="Commands">
						<For each={filteredCommands()}>
							{(command, index) => (
								<button
									id={`command-option-${index()}`}
									class="command-palette-option"
									type="button"
									data-variant="ghost"
									role="option"
									tabIndex={-1}
									aria-selected={index() === selectedIndex()}
									onMouseEnter={() => setSelectedIndex(index())}
									onClick={() => chooseCommand(command)}
								>
									<span class="command-name">{command.name}</span>
									<span class="command-arg">
										{command.argName ? `[${command.argName}]` : ""}
									</span>
									<span class="command-description">
										{command.description ?? command.category ?? ""}
									</span>
								</button>
							)}
						</For>
					</div>
				</Show>
			</div>
			<OverlayHintBar
				class="command-palette-footer"
				hints={
					argumentCommand()
						? ["Enter run", "Esc close"]
						: ["Up/Down navigate", "Enter run", "Esc close"]
				}
			/>
		</DialogFrame>
	);
}
