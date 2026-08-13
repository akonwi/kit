/** @jsxImportSource solid-js */
import { type JSX, onCleanup, onMount } from "solid-js";
import { DesktopStatusBar, MobileStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import { HeaderBar } from "./HeaderBar";
import { PendingSlot } from "./PendingSlot";
import { TranscriptPane } from "./TranscriptPane";
import { WorkspacePaneHost } from "./WorkspacePaneHost";

export function AppShell(): JSX.Element {
	let viewportFrame: number | null = null;

	onMount(() => {
		const viewport = window.visualViewport;
		const syncViewport = () => {
			viewportFrame = null;
			const root = document.documentElement;
			root.style.setProperty(
				"--kit-visual-viewport-height",
				`${viewport?.height ?? window.innerHeight}px`,
			);
			root.style.setProperty(
				"--kit-visual-viewport-offset-top",
				`${viewport?.offsetTop ?? 0}px`,
			);
		};
		const scheduleViewportSync = () => {
			if (viewportFrame !== null) return;
			viewportFrame = requestAnimationFrame(syncViewport);
		};
		syncViewport();
		viewport?.addEventListener("resize", scheduleViewportSync);
		viewport?.addEventListener("scroll", scheduleViewportSync);
		window.addEventListener("resize", scheduleViewportSync);
		onCleanup(() => {
			if (viewportFrame !== null) cancelAnimationFrame(viewportFrame);
			viewport?.removeEventListener("resize", scheduleViewportSync);
			viewport?.removeEventListener("scroll", scheduleViewportSync);
			window.removeEventListener("resize", scheduleViewportSync);
		});
	});

	return (
		<div class="app-shell">
			<HeaderBar />
			<WorkspacePaneHost
				primary={<TranscriptPane />}
				dock={
					<div class="composer-stack">
						<MobileStatusBar />
						<PendingSlot />
						<ComposerDock />
					</div>
				}
			/>
			<DesktopStatusBar />
		</div>
	);
}
