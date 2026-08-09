/** @jsxImportSource solid-js */
import type { JSX } from "solid-js";
import { BottomStatusBar } from "./BottomStatusBar";
import { ComposerDock } from "./ComposerDock";
import { HeaderBar } from "./HeaderBar";
import { PendingSlot } from "./PendingSlot";
import { TranscriptPane } from "./TranscriptPane";
import { WorkspacePaneHost } from "./WorkspacePaneHost";

export function AppShell(): JSX.Element {
	return (
		<div class="app-shell">
			<HeaderBar />
			<WorkspacePaneHost>
				<div class="primary-workspace">
					<TranscriptPane />
					<div class="composer-stack">
						<PendingSlot />
						<ComposerDock />
					</div>
				</div>
			</WorkspacePaneHost>
			<BottomStatusBar />
		</div>
	);
}
