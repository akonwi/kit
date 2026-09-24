# Release notices and notes

Kit checks GitHub Releases once per application startup without delaying the shell. If a stable release newer than the installed version exists, the header shows a persistent amber `Update available: vX.Y.Z` contribution. Clicking it opens the release-notes workspace panel; normal header overflow behavior keeps the action accessible on narrow terminals.

The check is advisory and fails silently when offline, rate-limited, or given an invalid response. It is disabled in print/headless mode.

## Release notes

Run `/release-notes` to open the workspace panel for the installed release. These notes are bundled into the Kit binary and remain available offline. If an update is available, use the panel's **open release** action to view it on GitHub.

The bundled notes live in `src/features/releases/current-release.ts`. They must be updated when preparing each release so they describe the version being published.
