---
name: release
description: Prepare and publish Kit CLI and/or native macOS app releases. Use when asked to release Kit, cut a CLI or macOS app release, publish a new version, or bump the package for release.
---

# Release skill

Kit's Go executable and native macOS app are **independent releases**. For an
ambiguous "release Kit" request, ask whether to release the CLI, the macOS app,
or both before tagging or publishing. For both, follow each path separately;
do not force matching product versions or let one release implicitly publish the
other. Do not build `apps/web` or change its package version or release notes.
Never tag, push, publish, or access signing credentials without authorization
for the relevant release.

## Go CLI release (`vX.Y.Z`)

Publish Kit's Go executable through GitHub Releases and the `kit` Homebrew
formula. The `v*` tag workflow builds **only** CLI artifacts.

### Steps

1. Inspect `git status --short`, the current branch, and recent release tags.
   Do not include unrelated changes. Release from reviewed, committed `main`.
   Read the release scope in `backlog/README.md` and the core/TUI backlogs to
   identify relevant changes and limitations. Open backlog items are tracked
   work, not automatic release blockers; report material limitations honestly.

2. Review commits since the last published release (`gh release list` and
   `git log <previous-tag>..HEAD`). Choose a patch for fixes and maintenance,
   minor for new capabilities; ask if ambiguous. Use a stable `vX.Y.Z` tag.
   The tag is the release version: there is no package.json version bump.
   `internal/version.Version` and `Commit` keep their development defaults;
   the workflow overrides them with linker flags. Compare the current session
   protocol with the previous stable tag. All breaking wire changes since that
   tag share one next protocol number; normalize accidental additional bumps
   before release and regenerate maintained clients.

3. Write release notes in `docs/releases/vX.Y.Z.md` before publishing, using
   [`docs/releases/TEMPLATE.md`](../../../docs/releases/TEMPLATE.md). The
   "What's changed" section may use the commits since the previous release as
   its changelog. Include an "Upgrade guide" only when the release has breaking
   changes; for runtime, protocol, SDK, or plugin breaks, give version-matched
   migration guidance that users and coding assistants can follow. Document
   relevant compatibility and platform limits. Review the notes against the
   shipped code; do not claim outstanding checks have passed. The workflow
   requires this file and publishes it verbatim.

   Validate before publishing:
   ```sh
   gofmt -l .
   go build ./...
   go vet ./...
   go test ./...
   go test -race ./...
   go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/*.yml
   git diff --check
   ```
   Formatting must produce no filenames. Use an isolated `KIT_HOME` for tests.
   CGO and a C/C++ toolchain are required for Tree-sitter (ADR 0021).

4. Dry-run the release build on the local platform before tagging. Substitute
   the intended version below. Use a temporary output directory, not tracked
   build artifacts:
   ```sh
   version=1.2.3
   commit=$(git rev-parse HEAD)
   out=$(mktemp -d)
   # On macOS, also export MACOSX_DEPLOYMENT_TARGET=14.0.
   CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X github.com/akonwi/kit/internal/version.Version=$version -X github.com/akonwi/kit/internal/version.Commit=$commit" -o "$out/kit" ./cmd/kit
   tar -czf "$out/kit.tar.gz" -C "$out" kit
   mkdir "$out/extracted"
   tar -xzf "$out/kit.tar.gz" -C "$out/extracted"
   KIT_HOME="$out/home" "$out/extracted/kit" version
   KIT_HOME="$out/home" "$out/extracted/kit" --help
   ```
   Assert version output is exactly `kit X.Y.Z (<full commit SHA>)` and the
   tarball contains only `kit`. Remove the temporary directory when done.
   This validates only the host platform, not all four release targets.

5. Commit any intended release preparation with a Conventional Commit and push
   `main`. No empty version-bump commit is needed. Ensure the working tree is
   clean and the intended commit is on `origin/main`. Only with authorization
   to publish, tag that commit and push the tag:
   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```
   `.github/workflows/release.yml` builds on native macOS/Linux arm64/amd64
   runners using Go from `go.mod`, with CGO enabled. macOS targets 14.0 or newer;
   Linux builds use Ubuntu 24.04 (glibc 2.39 baseline). Tree-sitter is compiled
   in; system libraries remain platform dependencies. Do not claim fully
   static Linux binaries or compatibility with older libc versions.

6. Watch the tag's workflow (`gh run list --workflow=release.yml`, then
   `gh run watch <run-id> --exit-status`). Each job packages only the Go `kit`
   executable and smoke-tests the extracted binary's version and help.
   The release job publishes `docs/releases/vX.Y.Z.md`, not generated PR summaries
   or web-bundled notes. Verify the published body matches the curated notes.
   Confirm all four `kit_vX.Y.Z_<platform>.tar.gz` assets exist:
   ```sh
   gh release view vX.Y.Z --json assets,body
   ```
   Platforms: `darwin_arm64`, `darwin_amd64`, `linux_arm64`, `linux_amd64`.

7. Update `../homebrew-tap/Formula/kit.rb` using downloaded release assets and
   their SHA-256 hashes. Update version, all four URLs/hashes, install logic
   (the archive now contains only `kit`, no `runtime`), OS requirements, and
   the version test. Inspect the formula before editing. Commit with subject
   `kit X.Y.Z` and push with release authorization. Verify:
   ```sh
   brew update && brew upgrade akonwi/tap/kit && brew test kit
   ```
   Verify manual extraction/install too. Follow the migration guidance and
   report any unverified distribution behavior without treating open backlog
   items as release gates.

8. Report the version, validation results and platform limits, release commit,
   tag, GitHub release URL, and Homebrew tap commit. Never imply a local dry run
   published a release or verified other platforms.

## Native macOS app release (`macos-vX.Y.Z`)

The app is a regular Apple Silicon, macOS 15+ release with a separate external
Kit server. Follow [ADR 0029](../../../docs/adrs/0029-distribute-native-macos-app-separately.md)
and [`apps/macos/README.md`](../../../apps/macos/README.md). The app release is **manual**:
`.github/workflows/macos.yml` builds a non-distributable PR staging app; it
does not sign, notarize, tag, or publish an app.

1. Check both the Kit and `../homebrew-tap` worktrees, current tags and published
   releases. Release only from reviewed, committed `main`. Choose the app's own
   `X.Y.Z` version (starting at `0.1.0`), not the CLI version. Review app changes
   since the previous `macos-v*` tag and ensure the source-derived
   `CFBundleVersion` build number increases. Check the generated wire protocol,
   `KitClientRelease` identity, and pinned `client_release` in
   `apps/macos/script/package_app.sh` against the actual server compatibility
   contract. For protocol 40, stable server releases >= 0.37.0 can be compatible
   without matching the app version; do not require one exact CLI release.
   Changes to the protocol or compatibility promise require explicit review.
2. Prepare curated, version-matched app notes for the reviewed release commit
   before tagging. Include its full source SHA, supported macOS/architecture,
   external-server requirement, protocol/release compatibility, manual
   install/update path, Cask status, and material limitations or skipped checks.
   The app release is manual: supply the finalized notes to `gh release create`
   with `--notes-file`; no workflow reads a tracked app notes file. If committing
   notes, do so **before** selecting the release commit—do not change or retag
   the source merely to put its own SHA in a tracked file. Confirm the intended
   tag and release commit before building.
3. Verify generated OpenAPI code (`apps/macos/script/generate_openapi.sh --check`), shell syntax (`bash -n apps/macos/script/build_and_run.sh
   apps/macos/script/package_app.sh`), `git diff --check`, and the full macOS
   suite from `apps/macos`:
   ```sh
   xcodebuild -scheme Kit -destination 'platform=macOS' \
     -derivedDataPath .build/xcode -skipPackagePluginValidation test
   ```
   Run the arm64 Release staging build and check its architecture, bundle
   version/build, source commit, client-release identity, and fixture exclusion
   as in `.github/workflows/macos.yml`. This staging app is ad-hoc signed and
   must **not** be published. Do not start, replace, or restart a production
   daemon to run compatibility checks; use an isolated `KIT_HOME` when a
   development server or database is needed.
4. With authorization, tag the reviewed clean commit `macos-vX.Y.Z`. From that
   exact checkout, with the operator's Developer ID Application identity in the
   keychain and the `kit` notarytool keychain profile, run:
   ```sh
   KIT_DEVELOPER_ID_APPLICATION='Developer ID Application: Akonwi Ngoh (M7B73F53MK)' \
     KIT_NOTARY_KEYCHAIN_PROFILE=kit \
     apps/macos/script/package_app.sh macos-vX.Y.Z
   ```
   The script builds arm64 Release, signs with hardened runtime, notarizes,
   staples, assesses Gatekeeper, and creates
   `apps/macos/dist/releases/kit_macos-vX.Y.Z_darwin_arm64.zip` and `.sha256`.
   It refuses dirty or untagged source; never publish the ad-hoc development
   bundle or expose keychain credentials in notes or logs.
5. Verify the ZIP checksum, extract it outside the checkout, and inspect the
   app's architecture, source commit, bundle version/build, `KitClientRelease`,
   Developer ID signature, stapled ticket, and Gatekeeper assessment. Exercise
   the installed artifact against a compatible running server and check
   incompatible-server recovery without altering that server. For upgrades,
   verify retained settings, drafts, and window restoration. Do not claim
   cross-version live testing unless it was actually done.
6. With push/publication authorization, push `main` and the app tag. Create a
   **draft** regular GitHub Release using the curated notes, ZIP, and checksum;
   use `--verify-tag --latest=false` so the macOS app does not replace the CLI's
   Latest release. Check tag target, draft body, asset names, sizes, and digests
   against the local candidate. Fresh-download verification of draft assets is
   the default distribution check; if the user explicitly waives it, record
   that it was not performed. Publish the draft only after the agreed checks
   pass (or explicitly waived checks are recorded). There is no Go release
   workflow for `macos-v*` tags.
7. Update `../homebrew-tap/Casks/kit-app.rb` using the **published** app asset
   URL and exact SHA-256. Leave `Formula/kit.rb` unchanged. Check macOS 15+
   and arm64 constraints, run `brew audit --cask --strict akonwi/tap/kit-app`
   and `brew style --cask akonwi/tap/kit-app` from a tap visible to Homebrew,
   and test `brew install --cask akonwi/tap/kit-app` (or `brew upgrade --cask`
   for an existing installation). Verify the installed `/Applications/Kit.app`
   metadata, signature, Gatekeeper assessment, and launch as appropriate. Push
   the tap change only with authorization. Installing the app must not install,
   start, or replace the CLI/server.
8. Report the app version, full source commit and tag, checksum, validation
   (including waived or untested checks), GitHub Release URL, Cask commit and
   installation result. Keep CLI and app release outcomes distinct.
