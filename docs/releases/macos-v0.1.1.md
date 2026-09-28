# Kit for macOS 0.1.1 — draft release notes

**Preparation only: do not publish this draft.** Insert the full reviewed source commit and ZIP SHA-256 after finalizing the source commit and notarized artifact; record any skipped verification before using these notes with `gh release create --notes-file`.

This patch release fixes duplicate transcript identity crashes and improves annotation presentation and recovery from stale file/diff evidence. Accepted annotations appear without a temporary live placeholder. The model picker now shows bounded context sizes.

## Install or upgrade

Requires **macOS 15+ on Apple Silicon**. The app does **not include, install, start, restart, or replace a server**. Install the Kit CLI separately, start a compatible server, and use `brew install --cask akonwi/tap/kit-app` (or `brew upgrade --cask akonwi/tap/kit-app` once the updated Cask is published). Alternatively, download `kit_macos-v0.1.1_darwin_arm64.zip`, extract `Kit.app`, and replace the existing app in `/Applications` after closing it. Updates are manual; there is no in-app updater. Preserve app settings, drafts, and saved windows when replacing the app, and verify restoration after upgrading.

The app speaks **session protocol 41** and identifies its client compatibility contract as Kit `0.38.0` independently of its own app version `0.1.1`. It can attach to a separately running canonical stable Kit server >= 0.38.0 that retains protocol 41; exact CLI/app version matching is not required. App 0.1.0 and server 0.37.x speak protocol 40 and cannot attach across this upgrade. Incompatibility is reported without replacing the running server; wait for active work to settle, upgrade the CLI separately, restart its daemon intentionally, then reconnect. Do not downgrade a server against migrated data.

Source commit: **TBD after release preparation is committed**.
Archive SHA-256: **TBD after signing, notarization, and ZIP verification**.

## Verification and limitations

The notarized, stapled, Developer ID signed ZIP, fresh draft-asset download, Gatekeeper assessment, compatible-server and incompatible-server tests, upgrade-state preservation, and Cask installation must be verified before publishing. No ad-hoc staging app is distributable. Intel macOS and a bundled server are not supported. Record any untested checks in the final notes; this draft asserts none have passed.
